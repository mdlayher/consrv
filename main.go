// Command consrv is a basic SSH to serial console bridge server for gokrazy.org
// appliances.
package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"time"

	"github.com/mdlayher/metricslite"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	// Create device mappings from the configuration file and open the serial
	// devices for the duration of the program's run.
	devices := make(map[string]*muxDevice, len(cfg.Devices))
	for _, d := range cfg.Devices {
		dev, err := openSerial(d.Name, d.Device, d.Baud, mm.deviceReadBytes, mm.deviceWriteBytes)
		if err != nil {
			log.Fatalf("failed to add device %q: %v", d.Name, err)
		}

		log.Printf("added device %q: %s (%d baud)", d.Name, d.Device, d.Baud)

		devices[d.Name] = newMuxDevice(dev)
		mm.deviceInfo(1.0, d.Name, d.Device, strconv.Itoa(d.Baud))
	}

	hostKey, err := ioutil.ReadFile("/perm/consrv/host_key")
	if err != nil {
		log.Fatalf("failed to read SSH host key: %v", err)
	}

	// Start the SSH server and configure the handler.
	// TODO: make configurable.

	srv, err := newSSHServer(hostKey, devices, cfg.Identities, mm)
	if err != nil {
		log.Fatalf("failed to create SSH server: %v", err)
	}

	var eg errgroup.Group

	eg.Go(func() error {
		const addr = ":2222"
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to listen for SSH: %v", err)
		}
		defer l.Close()

		log.Printf("starting SSH server on %q", addr)
		if err := srv.Serve(l); err != nil {
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
