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
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"
)

func TestMux(t *testing.T) {
	m, w := tempMux(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Make a fixed number of workers each consume a certain number of messages.
	const (
		nWorkers  = 4
		nMessages = 4
	)

	rs := make([]io.Reader, 0, nWorkers)
	for i := 0; i < nWorkers; i++ {
		rs = append(rs, m.Attach(ctx))
	}

	timer := time.AfterFunc(10*time.Second, func() {
		panic("test took too long")
	})
	defer timer.Stop()

	// Workers will fill in their own column in the matrix, so there is no need
	// for locking.
	//
	// Thanks @emrecanbati from Twitch for the suggestion: "we can create an
	// array and send index from writer then on reader side mark each index as
	// read."
	var (
		got [nWorkers][nMessages]string
		eg  errgroup.Group
	)

	for i, r := range rs {
		// Copy for goroutines.
		i, r := i, r

		eg.Go(func() error {
			// Consume messages and populate the correct matrix element per loop.
			b := make([]byte, 64)
			for j := 0; j < nMessages; j++ {
				n, err := r.Read(b)
				if err != nil {
					return fmt.Errorf("failed to read: %v", err)
				}

				got[i][j] = string(b[:n])
			}

			return nil
		})
	}

	for i := 0; i < nMessages; i++ {
		_, _ = io.WriteString(w, fmt.Sprintf("write %d", i))

		// TODO: this is a hack, we really should find a good way to allow the
		// reader group to acknowledge each write as a unit.
		time.Sleep(250 * time.Millisecond)
	}

	// All writes complete, wait for the goroutines to return.
	if err := eg.Wait(); err != nil {
		t.Fatalf("failed to wait: %v", err)
	}

	// Produce a matrix identical to the ones the workers would for comparison.
	var want [nWorkers][nMessages]string
	for i := 0; i < len(want); i++ {
		for j := 0; j < len(want[i]); j++ {
			want[i][j] = fmt.Sprintf("write %d", j)
		}
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("unexpected matrix (-want +got):\n%s", diff)
	}
}

func tempMux(t *testing.T) (*mux, io.Writer) {
	t.Helper()

	r, w := io.Pipe()
	m := newMux(r)

	t.Cleanup(func() {
		// The order here is important: closing the writer allows closing the
		// reader, and the mux would block until the reader can be closed.
		_ = w.Close()
		_ = r.Close()
		_ = m.Close()
	})

	return m, w
}
