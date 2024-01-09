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
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"sync"
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
		c            = flag.String("c", "consrv.toml", "path to consrv.toml configuration file")
		k            = flag.String("k", "host_key", "path to OpenSSH format host key file")
		mustPrivdrop = flag.Bool("experimental-drop-privileges", false, "[EXPERIMENTAL] run as an unprivileged process and chroot to an empty dir")
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

	numLogToStdout := 0
	for _, d := range cfg.Devices {
		if d.LogToStdout {
			numLogToStdout++
		}
	}
	var stdoutMu sync.Mutex

	for _, d := range cfg.Devices {
		dev, err := fs.openSerial(&d, mm.deviceReadBytes, mm.deviceWriteBytes)
		if err != nil {
			ll.Fatalf("failed to add device %q: %v", d.Name, err)
		}

		ll.Printf("configured device %s [log: %t]", dev, d.LogToStdout)

		mux := newMuxDevice(dev)
		devices[d.Name] = mux
		mm.deviceInfo(1.0, d.Name, d.Device, d.Serial, strconv.Itoa(d.Baud))
		if d.LogToStdout {
			var prefix string
			if numLogToStdout > 1 {
				// Disambiguate log messages when multiple devices are copied to
				// stdout.
				prefix = fmt.Sprintf("%s: ", d.Name)
			}
			rawReader := mux.m.Attach(context.Background())
			go func() {
				scanner := bufio.NewScanner(rawReader)
				for scanner.Scan() {
					stdoutMu.Lock()
					fmt.Println(prefix + scanner.Text())
					stdoutMu.Unlock()
				}
				if err := scanner.Err(); err != nil {
					ll.Printf("copying serial to stdout: %v", err)
				}
			}()
		}
	}

	privdrop := newPrivdropCond()

	// Start the SSH server.
	sshListener, err := net.Listen("tcp", cfg.Server.Address)
	if err != nil {
		ll.Fatalf("failed to listen for SSH server: %v", err)
	}

	sshSrv, err := newSSHServer(hostKey, devices, newIdentities(cfg, ll), ll, mm)
	if err != nil {
		ll.Fatalf("failed to create SSH server: %v", err)
	}

	var eg errgroup.Group

	eg.Go(func() error {
		defer sshListener.Close()

		if *mustPrivdrop {
			ll.Printf("SSH server waiting for privdrop")
			waitForCond(privdrop)
		}

		ll.Printf("SSH server starting")
		ll.Printf("starting SSH server on %q", sshListener.Addr())
		if err := sshSrv.Serve(sshListener); err != nil {
			return fmt.Errorf("failed to serve SSH: %v", err)
		}

		return nil
	})

	// Enable debug server if an address is set.
	if cfg.Debug.Address != "" {
		debugListener, err := net.Listen("tcp", cfg.Debug.Address)
		if err != nil {
			ll.Fatalf("failed to listen for HTTP debug server: %v", err)
		}

		eg.Go(func() error {
			defer debugListener.Close()

			if *mustPrivdrop {
				ll.Printf("debug HTTP waiting for privdrop")
				waitForCond(privdrop)
			}

			if err := serveDebug(cfg.Debug, reg, debugListener, ll); err != nil {
				return fmt.Errorf("failed to serve debug HTTP: %v", err)
			}

			return nil
		})
	}

	if *mustPrivdrop {
		dropPrivileges(privdrop, ll)
	}

	if err := eg.Wait(); err != nil {
		ll.Fatalf("failed to run: %v", err)
	}
}

// serveDebug starts the HTTP debug server with the input configuration.
func serveDebug(d debug, reg *prometheus.Registry, listener net.Listener, ll *log.Logger) error {
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

	return s.Serve(listener)
}
