// Command consrv is a basic SSH to serial console bridge server for gokrazy.org
// appliances.
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
)

// WIP WIP WIP, there's a lot more to do!
//
// TODO:
//  - Prometheus metrics
//  - remove hardcoded devices/paths for non-gokrazy machines
//  - capture and inspect/alert on kernel panics
//  - magic sysrq support
//  - signal handler to block until all connections close?

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

	// Create device mappings from the configuration file.
	devices := make(map[string]openFunc, len(cfg.Devices))
	for _, d := range cfg.Devices {
		log.Printf("added device %q: %s (%d baud)", d.Name, d.Device, d.Baud)
		devices[d.Name] = openSerial(d.Device, d.Baud)
	}

	dm := newDeviceMap(devices)

	// Start the SSH server and configure the handler.
	// TODO: make configurable.
	const addr = ":2222"
	srv, err := newSSHServer(addr, "/perm/consrv/host_key", cfg.Identities)
	if err != nil {
		log.Fatalf("failed to create SSH server: %v", err)
	}

	srv.Handle(func(s ssh.Session) {
		// Use usernames to map to valid serial devices, and ensure exclusive
		// access to a given serial device.
		dev, err := dm.Open(s)
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
		defer dev.Close()

		// Begin proxying between SSH and serial console until the SSH
		// connection closes or is broken.
		logf(s, "opened serial connection %q to %s", s.User(), dev.String())

		var eg errgroup.Group
		eg.Go(eofCopy(dev, s))
		eg.Go(eofCopy(s, dev))

		if err := eg.Wait(); err != nil {
			log.Printf("error proxying SSH/serial for %s: %v", s.RemoteAddr(), err)
		}

		// TODO: tidy up open sessions map based on user/source address
		_ = s.Exit(0)
		log.Printf("%s: closed serial connection %q to %s", s.RemoteAddr(), s.User(), dev.String())
	})

	log.Printf("starting SSH server on %q", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("failed to serve SSH: %v", err)
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

// eofCopy is like io.Copy but it consumes io.EOF errors and is specialized for
// errgroup use.
func eofCopy(w io.Writer, r io.Reader) func() error {
	return func() error {
		if _, err := io.Copy(w, r); err != nil && !errors.Is(err, io.EOF) {
			return err
		}

		return nil
	}
}
