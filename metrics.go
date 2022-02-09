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
			"name", "device", "serial", "baud",
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
