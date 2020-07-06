package main

import (
	"fmt"
	"io"
	"io/ioutil"
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
	d.reads(float64(n), d.device)
	return n, err
}

// Write implements io.ReadWriteCloser.
func (d *serialDevice) Write(b []byte) (int, error) {
	n, err := d.rwc.Write(b)
	d.writes(float64(n), d.device)
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
	glob     func(pattern string) ([]string, error)
	readFile func(file string) ([]byte, error)
	openPort func(cfg *serial.Config) (io.ReadWriteCloser, error)
}

// newFS creates a fs that operates on the real filesystem.
func newFS() *fs {
	return &fs{
		glob:     filepath.Glob,
		readFile: ioutil.ReadFile,
		openPort: func(cfg *serial.Config) (io.ReadWriteCloser, error) {
			return serial.OpenPort(cfg)
		},
	}
}

// openSerial opens a serial port and instruments it with metrics.
func (fs *fs) openSerial(d *rawDevice, reads, writes metricslite.Counter) (device, error) {
	if d.Serial != "" {
		// If the caller specified a serial number, use it to look up the
		// device's path.
		dev, err := fs.findttyUSBSerial(d.Serial)
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

// findttyUSBSerial looks up a device's path by its serial string.
func (fs *fs) findttyUSBSerial(serial string) (string, error) {
	matches, err := fs.glob("/dev/ttyUSB*")
	if err != nil {
		return "", err
	}

	for _, m := range matches {
		// filepath.Join would clean up the final path segment, so use
		// concatentation there instead.
		b, err := fs.readFile(filepath.Join("/sys/class/tty/", filepath.Base(m)) + "/device/../../serial")
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return "", err
		}

		if strings.TrimSpace(string(b)) == serial {
			return m, nil
		}
	}

	return "", fmt.Errorf("could not find device with serial %q: %w", serial, os.ErrNotExist)
}
