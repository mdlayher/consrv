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
	deviceSessions        metricslite.Gauge
	deviceUnknownSessions metricslite.Counter
}

func newMetrics(m metricslite.Interface) *metrics {
	return &metrics{
		deviceInfo: m.Gauge(
			"consrv_device_info",
			"Information metrics about each configured serial console device.",
			"name", "device", "baud",
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
	}
}

func (m *metrics) newSession(name string) func() {
	m.deviceSessions(float64(atomic.AddInt32(&m.sessions, 1)), name)
	return func() {
		m.deviceSessions(float64(atomic.AddInt32(&m.sessions, -1)), name)
	}
}
