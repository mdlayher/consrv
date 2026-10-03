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
	"bytes"
	"context"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func Test_lineSplitter(t *testing.T) {
	long := strings.Repeat("x", maxLogLine)

	tests := []struct {
		name   string
		chunks []string
		want   []string
	}{
		{
			name:   "line endings",
			chunks: []string{"a\nb\rc\r\nd\n\n\r\n"},
			want:   []string{"a", "b", "c", "d"},
		},
		{
			name:   "across chunks",
			chunks: []string{"hel", "lo\r", "\nwor", "ld"},
			want:   []string{"hello", "world"},
		},
		{
			name:   "long line",
			chunks: []string{long + "yz"},
			want:   []string{long, "yz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			ls := &lineSplitter{emit: func(line []byte) {
				got = append(got, string(line))
			}}

			for _, c := range tt.chunks {
				ls.write([]byte(c))
			}
			ls.flush()

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("unexpected lines (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_escapeLine(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{
			name: "plain",
			in:   []byte("login:\tok °C"),
			want: "login:\tok °C",
		},
		{
			name: "escape sequence",
			in:   []byte("\x1b[2J\x1b[1;1HSetup\x7f"),
			want: `\x1b[2J\x1b[1;1HSetup\x7f`,
		},
		{
			name: "invalid UTF-8",
			in:   []byte{0xc4, 0xc4, 'A'},
			want: `\xc4\xc4A`,
		},
		{
			name: "backslash",
			in:   []byte(`C:\x1b`),
			want: `C:\\x1b`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, escapeLine(tt.in)); diff != "" {
				t.Fatalf("unexpected escaped line (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMuxReaderShortBuffer(t *testing.T) {
	m, w := tempMux(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := m.Attach(ctx)

	go func() { _, _ = io.WriteString(w, "0123456789") }()

	var got []byte
	b := make([]byte, 3)
	for len(got) < 10 {
		n, err := r.Read(b)
		if err != nil {
			t.Fatalf("failed to read: %v", err)
		}
		got = append(got, b[:n]...)
	}

	if diff := cmp.Diff("0123456789", string(got)); diff != "" {
		t.Fatalf("unexpected data (-want +got):\n%s", diff)
	}
}

func TestLogDevice(t *testing.T) {
	m, w := tempMux(t)

	var out bytes.Buffer
	r := m.Attach(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		logDevice(r, &out, "router", log.New(io.Discard, "", 0))
	}()

	_, _ = io.WriteString(w, "boot\r\n\x1b[1;1Hlogin: ")
	_ = w.(io.Closer).Close()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("logDevice did not return after EOF")
	}

	want := "router: boot\nrouter: \\x1b[1;1Hlogin: \n"
	if diff := cmp.Diff(want, out.String()); diff != "" {
		t.Fatalf("unexpected log output (-want +got):\n%s", diff)
	}
}

func TestLogDeviceDoesNotBlockMux(t *testing.T) {
	m, w := tempMux(t)

	// A log writer which never returns, as if the journal had stopped.
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	go logDevice(
		m.Attach(context.Background()),
		writerFunc(func(b []byte) (int, error) {
			<-stuck
			return len(b), nil
		}),
		"router",
		log.New(io.Discard, "", 0),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := m.Attach(ctx)

	// Write far more lines than the log backlog holds; a session attached
	// alongside the log must still receive every one.
	const n = logBacklog * 4
	go func() {
		for i := 0; i < n; i++ {
			_, _ = io.WriteString(w, "line\n")
		}
	}()

	timer := time.AfterFunc(10*time.Second, func() {
		panic("session blocked by a stuck log writer")
	})
	defer timer.Stop()

	var got int
	b := make([]byte, 64)
	for got < n*len("line\n") {
		c, err := r.Read(b)
		if err != nil {
			t.Fatalf("failed to read: %v", err)
		}
		got += c
	}
}

type writerFunc func(b []byte) (int, error)

func (fn writerFunc) Write(b []byte) (int, error) { return fn(b) }
