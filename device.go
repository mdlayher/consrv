package main

import (
	"io"
	"os"
	"sync"

	"github.com/gliderlabs/ssh"
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
	// TODO: embedding is lazy and error prone.
	*serial.Port
	device string
}

// String returns the string representation of a serialDevice.
func (d *serialDevice) String() string { return d.device }

// An openFunc is a function which opens a preconfigured device.
type openFunc func() (device, error)

// openSerial produces an openFunc which produces serialDevices.
func openSerial(name string, baud int) openFunc {
	return func() (device, error) {
		port, err := serial.OpenPort(&serial.Config{
			Name: name,
			Baud: baud,
		})
		if err != nil {
			return nil, err
		}

		return &serialDevice{
			Port:   port,
			device: name,
		}, nil
	}
}

// A deviceMap looks up functions to open configured devices given an input
// SSH session's parameters.
type deviceMap struct {
	mu   sync.Mutex
	m    map[string]openFunc
	open map[string]session
}

// A session stores information about an open SSH session.
type session struct {
	s   ssh.Session
	dev io.Closer
}

// newDeviceMap creates a deviceMap from the input device mappings.
func newDeviceMap(m map[string]openFunc) *deviceMap {
	return &deviceMap{
		m:    m,
		open: make(map[string]session),
	}
}

// Open attempts to open a connection to a device using parameters from an SSH
// session, ensuring the device exists and providing exclusive control of the
// device to a new client.
func (dm *deviceMap) Open(s ssh.Session) (device, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	fn, ok := dm.m[s.User()]
	if !ok {
		return nil, os.ErrNotExist
	}

	if ps, ok := dm.open[s.User()]; ok {
		// There was a previous session open, warn that client that their
		// session will be terminated and note that a session was terminated to
		// the new client and logging.
		logf(ps.s, "terminating, new connection opened for %q from client %s", s.User(), s.RemoteAddr())
		_ = ps.s.Exit(1)
		logf(s, "terminated open connection for %q from client %s", s.User(), ps.s.RemoteAddr())
	}

	// Okay, ready to open the device and create a new session for it.
	d, err := fn()
	if err != nil {
		return nil, err
	}

	dm.open[s.User()] = session{
		s:   s,
		dev: d,
	}

	return d, nil
}
