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

package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/mdlayher/metricslite"
	"github.com/tarm/serial"
)

// A device is a handle to a console device.
type device interface {
	io.ReadWriteCloser
	String() string
}

var _ device = &serialDevice{}

// A serialDevice is a device implemented using a serial port.
type serialDevice struct {
	rwc                  io.ReadWriteCloser
	name, device, serial string
	baud                 int
	reads, writes        metricslite.Counter
}

// Close implements io.ReadWriteCloser.
func (d *serialDevice) Close() error { return d.rwc.Close() }

// Read implements io.ReadWriteCloser.
func (d *serialDevice) Read(b []byte) (int, error) {
	n, err := d.rwc.Read(b)
	d.reads(float64(n), d.name)
	return n, err
}

// Write implements io.ReadWriteCloser.
func (d *serialDevice) Write(b []byte) (int, error) {
	n, err := d.rwc.Write(b)
	d.writes(float64(n), d.name)
	return n, err
}

// String returns the string representation of a serialDevice.
func (d *serialDevice) String() string {
	return fmt.Sprintf("%q: path: %q, serial: %q, baud: %d",
		d.name, d.device, d.serial, d.baud)
}

// A muxDevice is a device with multiplexed reads.
type muxDevice struct {
	m *mux
	device
}

// newMuxDevice wraps a device with a mux.
func newMuxDevice(d device) *muxDevice {
	return &muxDevice{
		m:      newMux(d),
		device: d,
	}
}

// Close cleans up the device and mux.
func (d *muxDevice) Close() error {
	err1 := d.device.Close()
	err2 := d.m.Close()

	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}

	return nil
}

// An fs abstracts filesystem operations. Most callers should use newFS to
// construct an fs that operates on the real filesystem.
type fs struct {
	serialToDevice map[string]string

	glob     func(pattern string) ([]string, error)
	readFile func(file string) ([]byte, error)
	openPort func(cfg *serial.Config) (io.ReadWriteCloser, error)
}

// newFS creates a fs that operates on the real filesystem.
func newFS(ll *log.Logger) (*fs, error) {
	fs := &fs{
		glob:     filepath.Glob,
		readFile: ioutil.ReadFile,
		openPort: func(cfg *serial.Config) (io.ReadWriteCloser, error) {
			return serial.OpenPort(cfg)
		},
	}

	return fs, fs.init(ll)
}

// init initializes a fs by enumerating the available devices and logging them
// so the user may more easily configure them.
func (fs *fs) init(ll *log.Logger) error {
	fs.serialToDevice = make(map[string]string)
	eds, err := fs.enumerate()
	if err != nil {
		return err
	}

	for _, ed := range eds {
		ll.Printf("found device: path: %q, serial: %q", ed.device, ed.serial)
	}

	return nil
}

// An enumerated device is a device found in the filesystem.
type enumeratedDevice struct {
	device, serial string
}

// enumerate enumerates all available serial devices from the filesystem.
func (fs *fs) enumerate() ([]enumeratedDevice, error) {
	if fs.glob == nil {
		// No glob function, can't enumerate devices.
		return nil, nil
	}

	matches, err := fs.glob("/dev/ttyUSB*")
	if err != nil {
		return nil, err
	}

	var eds []enumeratedDevice
	for _, m := range matches {
		// filepath.Join would clean up the final path segment, so use
		// concatentation there instead.
		b, err := fs.readFile(filepath.Join("/sys/class/tty/", filepath.Base(m)) + "/device/../../serial")
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, err
		}

		serial := strings.TrimSpace(string(b))
		eds = append(eds, enumeratedDevice{
			device: m,
			serial: serial,
		})

		fs.serialToDevice[serial] = m
	}

	return eds, nil
}

// openSerial opens a serial port and instruments it with metrics.
func (fs *fs) openSerial(d *rawDevice, reads, writes metricslite.Counter) (device, error) {
	if d.Serial != "" {
		// If the caller specified a serial number, use it to look up the
		// device's path.
		dev, ok := fs.serialToDevice[d.Serial]
		if !ok {
			return nil, os.ErrNotExist
		}

		d.Device = dev
	}

	// name is the friendly name, while device is the raw device/port path.
	rwc, err := fs.openPort(&serial.Config{
		Name: d.Device,
		Baud: d.Baud,
	})
	if err != nil {
		return nil, err
	}

	return &serialDevice{
		rwc:    rwc,
		name:   d.Name,
		device: d.Device,
		serial: d.Serial,
		baud:   d.Baud,
		reads:  reads,
		writes: writes,
	}, nil
}
