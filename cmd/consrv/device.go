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
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mdlayher/metricslite"
	"github.com/tarm/serial"
)

// A device is a handle to a console device.
type device interface {
	io.ReadWriteCloser
	fmt.Stringer
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
	// serialToDevices maps a USB serial number to one or more devices.
	// Multi-port adapters expose several ports which share a single serial
	// number, distinguished only by their USB interface numbers.
	serialToDevices map[string][]enumeratedDevice

	glob     func(pattern string) ([]string, error)
	readFile func(file string) ([]byte, error)
	openPort func(cfg *serial.Config) (io.ReadWriteCloser, error)
}

// newFS creates a fs that operates on the real filesystem.
func newFS(ll *log.Logger) (*fs, error) {
	fs := &fs{
		glob:     filepath.Glob,
		readFile: os.ReadFile,
		openPort: func(cfg *serial.Config) (io.ReadWriteCloser, error) {
			return serial.OpenPort(cfg)
		},
	}

	return fs, fs.init(ll)
}

// init initializes a fs by enumerating the available devices and logging them
// so the user may more easily configure them.
func (fs *fs) init(ll *log.Logger) error {
	fs.serialToDevices = make(map[string][]enumeratedDevice)
	eds, err := fs.enumerate()
	if err != nil {
		return err
	}

	for _, ed := range eds {
		ll.Printf("found device: path: %q, serial: %q, interface: %d", ed.device, ed.serial, ed.iface)
	}

	for serial, devs := range fs.serialToDevices {
		if len(devs) > 1 {
			ll.Printf(`warning: %d devices share serial %q, set "interface" to select a specific port`, len(devs), serial)
		}
	}

	return nil
}

// An enumerated device is a device found in the filesystem.
type enumeratedDevice struct {
	device, serial string
	iface          int
}

// enumerate enumerates all available serial devices from the filesystem.
func (fs *fs) enumerate() ([]enumeratedDevice, error) {
	if fs.glob == nil {
		// No glob function, can't enumerate devices.
		return nil, nil
	}

	// Traverse known serial device patterns and attach suffixes where their
	// serial and USB interface numbers may be found.
	sms := []serialMatch{
		{
			Pattern:     "/dev/ttyUSB*",
			Suffix:      "/device/../../serial",
			IfaceSuffix: "/device/../bInterfaceNumber",
		},
		{
			Pattern:     "/dev/ttyACM*",
			Suffix:      "/device/../serial",
			IfaceSuffix: "/device/bInterfaceNumber",
		},
	}

	var devices []enumeratedDevice
	for _, sm := range sms {
		devs, err := fs.match(sm)
		if err != nil {
			return nil, err
		}

		devices = append(devices, devs...)
	}

	return devices, nil
}

// A serialMatch matches a serial device type by Pattern and reads its serial
// number using Suffix and its USB interface number using IfaceSuffix.
type serialMatch struct {
	Pattern, Suffix, IfaceSuffix string
}

// match walks a single serialMatch to enumerate devices.
func (fs *fs) match(sm serialMatch) ([]enumeratedDevice, error) {
	matches, err := fs.glob(sm.Pattern)
	if err != nil {
		return nil, err
	}

	var eds []enumeratedDevice
	for _, m := range matches {
		// filepath.Join would clean up the final path segment, so use
		// concatentation there instead.
		base := filepath.Join("/sys/class/tty/", filepath.Base(m))

		b, err := fs.readFile(base + sm.Suffix)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, err
		}
		serial := strings.TrimSpace(string(b))

		iface, err := fs.readIface(base + sm.IfaceSuffix)
		if err != nil {
			return nil, fmt.Errorf("failed to read USB interface number for %q: %v", m, err)
		}

		ed := enumeratedDevice{
			device: m,
			serial: serial,
			iface:  iface,
		}

		eds = append(eds, ed)
		fs.serialToDevices[serial] = append(fs.serialToDevices[serial], ed)
	}

	return eds, nil
}

// readIface reads a USB interface number from file, assuming interface zero
// for devices which do not expose one.
func (fs *fs) readIface(file string) (int, error) {
	b, err := fs.readFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}

		return 0, err
	}

	// bInterfaceNumber is hexadecimal per the USB specification.
	v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 16, 8)
	if err != nil {
		return 0, err
	}

	return int(v), nil
}

// errAmbiguousSerial is returned when a serial number is shared by multiple
// devices and no USB interface number was configured to select one of them.
var errAmbiguousSerial = errors.New(`serial number is shared by multiple devices, set "interface" to select one`)

// findSerial looks up a device's path by its configured serial number and
// optional USB interface number, for multi-port adapters whose ports share a
// single serial number.
func (fs *fs) findSerial(d *rawDevice) (string, error) {
	eds := fs.serialToDevices[d.Serial]
	if len(eds) == 0 {
		return "", os.ErrNotExist
	}

	if d.Interface == nil {
		if len(eds) > 1 {
			ifaces := make([]string, 0, len(eds))
			for _, ed := range eds {
				ifaces = append(ifaces, strconv.Itoa(ed.iface))
			}

			return "", fmt.Errorf("%w: found interfaces: %s", errAmbiguousSerial, strings.Join(ifaces, ", "))
		}

		return eds[0].device, nil
	}

	for _, ed := range eds {
		if ed.iface == *d.Interface {
			return ed.device, nil
		}
	}

	return "", fmt.Errorf("no device with USB interface number %d: %w", *d.Interface, os.ErrNotExist)
}

// openSerial opens a serial port and instruments it with metrics.
func (fs *fs) openSerial(d *rawDevice, reads, writes metricslite.Counter) (device, error) {
	if d.Serial != "" {
		// If the caller specified a serial number, use it to look up the
		// device's path.
		dev, err := fs.findSerial(d)
		if err != nil {
			return nil, err
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
