package main

import (
	"io"
	"os"

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

// A deviceMap maps SSH usernames to muxDevices.
type deviceMap struct {
	m map[string]*muxDevice
}

// newDeviceMap creates a deviceMap from the input device mappings.
func newDeviceMap(mapping map[string]openFunc) (*deviceMap, error) {
	dm := &deviceMap{
		m: make(map[string]*muxDevice, len(mapping)),
	}

	for dev, fn := range mapping {
		d, err := fn()
		if err != nil {
			return nil, err
		}

		dm.m[dev] = newMuxDevice(d)
	}

	return dm, nil
}

// Open determines if a device exists given an input username.
func (dm *deviceMap) Open(user string) (*muxDevice, error) {
	mux, ok := dm.m[user]
	if !ok {
		return nil, os.ErrNotExist
	}

	return mux, nil
}
