package main

import (
	"io"

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
	p             *serial.Port
	name, device  string
	reads, writes metricslite.Counter
}

// Close implements io.ReadWriteCloser.
func (d *serialDevice) Close() error { return d.p.Close() }

// Read implements io.ReadWriteCloser.
func (d *serialDevice) Read(b []byte) (int, error) {
	n, err := d.p.Read(b)
	d.reads(float64(n), d.device)
	return n, err
}

// Write implements io.ReadWriteCloser.
func (d *serialDevice) Write(b []byte) (int, error) {
	n, err := d.p.Write(b)
	d.writes(float64(n), d.device)
	return n, err
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

// openSerial opens a serial port and instruments it with metrics.
func openSerial(name, device string, baud int, reads, writes metricslite.Counter) (device, error) {
	// name is the friendly name, while device is the raw device/port path.
	port, err := serial.OpenPort(&serial.Config{
		Name: device,
		Baud: baud,
	})
	if err != nil {
		return nil, err
	}

	return &serialDevice{
		p:      port,
		name:   name,
		device: device,
		reads:  reads,
		writes: writes,
	}, nil
}
