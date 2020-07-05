package main

import (
	"sync/atomic"

	"github.com/mdlayher/metricslite"
)

// metrics contains metrics for a consrv server.
type metrics struct {
	// Atomics must come first.
	sessions int32

	deviceInfo            metricslite.Gauge
	deviceAuthentications metricslite.Counter
	deviceSessions        metricslite.Gauge
	deviceUnknownSessions metricslite.Counter
	deviceReadBytes       metricslite.Counter
	deviceWriteBytes      metricslite.Counter
}

func newMetrics(m metricslite.Interface) *metrics {
	if m == nil {
		m = metricslite.Discard()
	}

	return &metrics{
		deviceInfo: m.Gauge(
			"consrv_device_info",
			"Information metrics about each configured serial console device.",
			"name", "device", "baud",
		),

		deviceAuthentications: m.Counter(
			"consrv_device_authentications_total",
			"The total number of accepted and rejected SSH sessions for a serial console device.",
			"name",
		),

		deviceSessions: m.Gauge(
			"consrv_device_sessions",
			"The number of active SSH sessions connected to a serial console device.",
			"name",
		),

		deviceUnknownSessions: m.Counter(
			"consrv_device_unknown_sessions_total",
			"The total number of SSH sessions which attempted to open a non-existent device.",
		),

		deviceReadBytes: m.Counter(
			"consrv_device_read_bytes_total",
			"The total number of bytes read from a serial device.",
			"name",
		),

		deviceWriteBytes: m.Counter(
			"consrv_device_write_bytes_total",
			"The total number of bytes written to a serial device.",
			"name",
		),
	}
}

func (m *metrics) newSession(name string) func() {
	m.deviceSessions(float64(atomic.AddInt32(&m.sessions, 1)), name)
	return func() {
		m.deviceSessions(float64(atomic.AddInt32(&m.sessions, -1)), name)
	}
}
