// Command consrv is a basic SSH to serial console bridge server for gokrazy.org
// appliances.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"time"

	"github.com/dolmen-go/contextio"
	"github.com/gliderlabs/ssh"
	"github.com/mdlayher/metricslite"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
)

// WIP WIP WIP, there's a lot more to do!
//
// TODO:
//  - remove hardcoded devices/paths for non-gokrazy machines
//  - capture and inspect/alert on kernel panics
//  - magic sysrq support
//  - signal handler to block until all connections close?
//  - support for detecting gokrazy build tag

func main() {
	f, err := os.Open("/perm/consrv/consrv.toml")
	if err != nil {
		log.Fatalf("failed to open config file: %v", err)
	}
	defer f.Close()

	cfg, err := parseConfig(f)
	if err != nil {
		log.Fatalf("failed to parse config: %v", err)
	}
	_ = f.Close()

	// Set up Prometheus metrics for the server.
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(
		prometheus.NewBuildInfoCollector(),
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)

	mm := newMetrics(metricslite.NewPrometheus(reg))

	// Create device mappings from the configuration file.
	devices := make(map[string]openFunc, len(cfg.Devices))
	for _, d := range cfg.Devices {
		log.Printf("added device %q: %s (%d baud)", d.Name, d.Device, d.Baud)
		devices[d.Name] = openSerial(d.Device, d.Baud)
		mm.deviceInfo(1.0, d.Name, d.Device, strconv.Itoa(d.Baud))
	}

	// Open serial devices for the duration of the program run.
	dm, err := newDeviceMap(devices)
	if err != nil {
		log.Fatalf("failed to open devices: %v", err)
	}

	// Start the SSH server and configure the handler.
	// TODO: make configurable.
	const sshAddr = ":2222"
	srv, err := newSSHServer(sshAddr, "/perm/consrv/host_key", cfg.Identities)
	if err != nil {
		log.Fatalf("failed to create SSH server: %v", err)
	}

	srv.Handle(func(s ssh.Session) {
		// Use usernames to map to valid device multiplexers.
		mux, err := dm.Open(s.User())
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// No such connection.
				logf(s, "exiting, unknown connection %q", s.User())
			} else {
				logf(s, "exiting, failed to open connection %q: %v", s.User(), err)
			}

			_ = s.Exit(1)
			return
		}

		done := mm.newSession(s.User())
		defer done()

		// Begin proxying between SSH and serial console mux until the SSH
		// connection closes or is broken.
		logf(s, "opened serial connection %q to %s", s.User(), mux.String())

		ctx, cancel := context.WithCancel(s.Context())
		defer cancel()

		// Create a new io.Reader handle from the mux for this client, so it
		// will receive the same output as other clients for the duration of its
		// session.
		r := mux.m.Attach(ctx)

		var eg errgroup.Group
		eg.Go(eofCopy(ctx, mux, s))
		eg.Go(eofCopy(ctx, s, r))

		if err := eg.Wait(); err != nil {
			log.Printf("error proxying SSH/serial for %s: %v", s.RemoteAddr(), err)
		}

		_ = s.Exit(0)
		log.Printf("%s: closed serial connection %q to %s", s.RemoteAddr(), s.User(), mux.String())
	})

	var eg errgroup.Group

	eg.Go(func() error {
		log.Printf("starting SSH server on %q", sshAddr)
		if err := srv.ListenAndServe(); err != nil {
			return fmt.Errorf("failed to serve SSH: %v", err)
		}

		return nil
	})

	eg.Go(func() error {
		// TODO: move to configuration file, enabling and disabling of
		// Prometheus and/or pprof handlers.
		//
		// Also consider
		// https://godoc.org/github.com/gokrazy/gokrazy#PrivateInterfaceAddrs
		// when running on gokrazy.
		const addr = ":9288"

		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		log.Printf("starting HTTP debug server on %q", addr)

		s := &http.Server{
			Addr:        addr,
			ReadTimeout: 1 * time.Second,
			Handler:     mux,
		}

		if err := s.ListenAndServe(); err != nil {
			return fmt.Errorf("failed to serve HTTP: %v", err)
		}

		return nil
	})

	if err := eg.Wait(); err != nil {
		log.Fatalf("failed to run: %v", err)
	}
}

// logf outputs a formatted log message to both stderr and an SSH client.
func logf(s ssh.Session, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Printf("%s: %s", s.RemoteAddr(), msg)
	fmt.Fprintf(s, "consrv> %s\n", msg)
}

// newSSHServer creates an SSH server which will bind to the specified address
// and use the input host key and authorized key files.
func newSSHServer(addr, hostKey string, ids []identity) (*ssh.Server, error) {
	srv := &ssh.Server{Addr: addr}
	srv.SetOption(ssh.HostKeyFile(hostKey))

	authorized := make(map[string]struct{})
	for _, id := range ids {
		f := gossh.FingerprintSHA256(id.PublicKey)
		log.Printf("added identity %q: %s", id.Name, f)
		authorized[f] = struct{}{}
	}

	srv.SetOption(ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
		// Is this client's key authorized for access?
		_, ok := authorized[gossh.FingerprintSHA256(key)]

		action := "rejected"
		if ok {
			action = "accepted"
		}

		log.Printf("%s: %s public key authentication for %s", ctx.RemoteAddr(), action, gossh.FingerprintSHA256(key))
		return ok
	}))

	return srv, nil
}

// eofCopy is a context-aware io.Copy that consumes io.EOF errors and is
// specialized for errgroup use.
func eofCopy(ctx context.Context, w io.Writer, r io.Reader) func() error {
	return func() error {
		_, err := io.Copy(
			contextio.NewWriter(ctx, w),
			contextio.NewReader(ctx, r),
		)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}

		return nil
	}
}
