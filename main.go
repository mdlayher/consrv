// Copyright 2020 Matt Layher and Michael Stapelberg
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
//  - capture and inspect/alert on kernel panics
//  - magic sysrq support
//  - signal handler to block until all connections close?

func main() {
	// Config/host key paths are only configurable on non-gokrazy platforms.
	cfgFile, keyFile := filePaths()

	ll := log.New(os.Stderr, "", log.LstdFlags)

	f, err := os.Open(cfgFile)
	if err != nil {
		ll.Fatalf("failed to open config file: %v", err)
	}
	defer f.Close()

	cfg, err := parseConfig(f)
	if err != nil {
		ll.Fatalf("failed to parse config: %v", err)
	}
	_ = f.Close()

	hostKey, err := ioutil.ReadFile(keyFile)
	if err != nil {
		ll.Fatalf("failed to read SSH host key: %v", err)
	}

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

	// Start the SSH server and configure the handler.
	// TODO: make configurable.

	srv, err := newSSHServer(hostKey, devices, newIdentities(cfg, ll), ll, mm)
	if err != nil {
		ll.Fatalf("failed to create SSH server: %v", err)
	}

	var eg errgroup.Group

	eg.Go(func() error {
		const addr = ":2222"
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to listen for SSH: %v", err)
		}
		defer l.Close()

		ll.Printf("starting SSH server on %q", addr)
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

		ll.Printf("starting HTTP debug server on %q", addr)

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
		ll.Fatalf("failed to run: %v", err)
	}
}
