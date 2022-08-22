// Copyright 2020-2022 Matt Layher and Michael Stapelberg
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Command consrv is a basic SSH to serial console bridge server for gokrazy.org
// appliances.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"time"

	"github.com/mdlayher/metricslite"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
)

// TODO:
//  - capture and inspect/alert on kernel panics
//  - magic sysrq support
//  - signal handler to block until all connections close?

func main() {
	var (
		c = flag.String("c", "consrv.toml", "path to consrv.toml configuration file")
		k = flag.String("k", "host_key", "path to OpenSSH format host key file")
	)

	flag.Parse()

	cfgFilePaths := []string{
		*c,
		"/etc/consrv/consrv.toml",
		"/perm/consrv/consrv.toml",
		"consrv.toml",
	}
	keyFilePaths := []string{
		*k,
		"/etc/consrv/host_key",
		"/perm/consrv/host_key",
		"host_key",
	}

	ll := log.New(os.Stderr, "", log.LstdFlags)

	var cfg *config
	for _, cfgFile := range cfgFilePaths {
		f, err := os.Open(cfgFile)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			ll.Fatalf("failed to open config file: %v", err)
		}
		defer f.Close()
		ll.Printf("loading configuration from %s", cfgFile)

		cfg, err = parseConfig(f)
		if err != nil {
			ll.Fatalf("failed to parse config: %v", err)
		}
		_ = f.Close()
		break
	}
	if cfg == nil {
		ll.Fatalf("no config file could be opened")
	}

	var hostKey []byte
	for _, keyFile := range keyFilePaths {
		var err error
		hostKey, err = os.ReadFile(keyFile)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			ll.Fatalf("failed to read SSH host key: %v", err)
		}
		ll.Printf("loading host key from %s", keyFile)
		break
	}

	// Set up Prometheus metrics for the server.
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(
		collectors.NewBuildInfoCollector(),
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	mm := newMetrics(metricslite.NewPrometheus(reg))

	// Create device mappings from the configuration file and open the serial
	// devices for the duration of the program's run.
	devices := make(map[string]*muxDevice, len(cfg.Devices))
	fs, err := newFS(ll)
	if err != nil {
		ll.Fatalf("failed to open filesystem: %v", err)
	}

	for _, d := range cfg.Devices {
		dev, err := fs.openSerial(&d, mm.deviceReadBytes, mm.deviceWriteBytes)
		if err != nil {
			ll.Fatalf("failed to add device %q: %v", d.Name, err)
		}

		ll.Printf("configured device %s", dev)

		devices[d.Name] = newMuxDevice(dev)
		mm.deviceInfo(1.0, d.Name, d.Device, d.Serial, strconv.Itoa(d.Baud))
	}

	// Start the SSH server.
	srv, err := newSSHServer(hostKey, devices, newIdentities(cfg, ll), ll, mm)
	if err != nil {
		ll.Fatalf("failed to create SSH server: %v", err)
	}

	var eg errgroup.Group

	eg.Go(func() error {
		l, err := net.Listen("tcp", cfg.Server.Address)
		if err != nil {
			return fmt.Errorf("failed to listen for SSH: %v", err)
		}
		defer l.Close()

		ll.Printf("starting SSH server on %q", cfg.Server.Address)
		if err := srv.Serve(l); err != nil {
			return fmt.Errorf("failed to serve SSH: %v", err)
		}

		return nil
	})

	// Enable debug server if an address is set.
	if cfg.Debug.Address != "" {
		eg.Go(func() error {
			if err := serveDebug(cfg.Debug, reg, ll); err != nil {
				return fmt.Errorf("failed to serve debug HTTP: %v", err)
			}

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		ll.Fatalf("failed to run: %v", err)
	}
}

// serveDebug starts the HTTP debug server with the input configuration.
func serveDebug(d debug, reg *prometheus.Registry, ll *log.Logger) error {
	mux := http.NewServeMux()

	if d.Prometheus {
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	}

	if d.PProf {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	ll.Printf("starting HTTP debug server on %q [prometheus: %t, pprof: %t]",
		d.Address, d.Prometheus, d.PProf)

	s := &http.Server{
		Addr:        d.Address,
		ReadTimeout: 1 * time.Second,
		Handler:     mux,
	}

	return s.ListenAndServe()
}
