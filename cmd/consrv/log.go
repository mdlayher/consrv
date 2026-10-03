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
	"log"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	// maxLogLine is the longest line logged before it is split. Firmware
	// setup screens draw with cursor movement and rarely end a line.
	maxLogLine = 4096

	// logFlushInterval is how long a partial line waits for more output, so
	// a prompt with no line ending is still logged.
	logFlushInterval = time.Second

	// logBacklog is how many reads from a device wait to be logged before
	// further reads are dropped.
	logBacklog = 64
)

// A lockedWriter serializes Write calls from multiple goroutines.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// Write implements io.Writer.
func (lw *lockedWriter) Write(b []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(b)
}

// logDevice logs the output of the device named name, read from r, to w as
// lines prefixed with the device's name, until r returns an error.
//
// r is a muxReader, which blocks every client of its device until it is read,
// so logDevice reads r continuously and drops output rather than block when w
// falls behind.
func logDevice(r io.Reader, w io.Writer, name string, ll *log.Logger) {
	var (
		chunks  = make(chan []byte, logBacklog)
		dropped atomic.Int64
	)

	go func() {
		defer close(chunks)

		b := make([]byte, 8192)
		for {
			n, err := r.Read(b)
			if n > 0 {
				select {
				case chunks <- append([]byte(nil), b[:n]...):
				default:
					dropped.Add(int64(n))
				}
			}
			if err != nil {
				if err != io.EOF {
					ll.Printf("reading device %q for logging: %v", name, err)
				}
				return
			}
		}
	}()

	lines := &lineSplitter{emit: func(line []byte) {
		_, _ = fmt.Fprintf(w, "%s: %s\n", name, escapeLine(line))
	}}

	timer := time.NewTimer(logFlushInterval)
	defer timer.Stop()

	for {
		select {
		case c, ok := <-chunks:
			if !ok {
				lines.flush()
				return
			}

			if n := dropped.Swap(0); n > 0 {
				ll.Printf("dropped %d bytes of device %q output, logging fell behind", n, name)
			}

			lines.write(c)
			timer.Reset(logFlushInterval)
		case <-timer.C:
			lines.flush()
		}
	}
}

// A lineSplitter splits device output into lines on any of "\n", "\r", or
// "\r\n", skipping empty lines and splitting lines longer than maxLogLine.
type lineSplitter struct {
	buf  []byte
	emit func(line []byte)
}

// write consumes b, emitting each line it completes.
func (ls *lineSplitter) write(b []byte) {
	for _, c := range b {
		switch c {
		case '\n', '\r':
			ls.flush()
		default:
			ls.buf = append(ls.buf, c)
			if len(ls.buf) == maxLogLine {
				ls.flush()
			}
		}
	}
}

// flush emits any partial line.
func (ls *lineSplitter) flush() {
	if len(ls.buf) == 0 {
		return
	}

	ls.emit(ls.buf)
	ls.buf = ls.buf[:0]
}

// escapeLine escapes control characters, backslashes, and bytes which are
// not valid UTF-8 as \xNN or \\, so terminal escape sequences and legacy
// character sets survive as readable text.
func escapeLine(b []byte) string {
	out := make([]byte, 0, len(b))
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		switch {
		case r == utf8.RuneError && size == 1, r < 0x20 && r != '\t', r == 0x7f:
			out = fmt.Appendf(out, `\x%02x`, b[0])
		case r == '\\':
			out = append(out, `\\`...)
		default:
			out = append(out, b[:size]...)
		}

		b = b[size:]
	}

	return string(out)
}
