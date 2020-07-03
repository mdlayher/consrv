package main

import (
	"context"
	"io"
	"sync"

	"golang.org/x/sync/errgroup"
)

// A mux is a multiplexer over an input io.Reader which provides identical
// output to any attached muxReaders.
type mux struct {
	mu      sync.Mutex
	id      int
	clients map[int]client

	eg errgroup.Group
}

// newMux creates a mux over the input io.Reader.
func newMux(r io.Reader) *mux {
	m := &mux{clients: make(map[int]client)}

	m.eg.Go(func() error {
		// Read continuously from the device and pass any data and/or errors to
		// each of the attached clients.
		b := make([]byte, 8192)
		for {
			n, err := r.Read(b)
			if err == io.EOF || err == io.ErrClosedPipe {
				// TODO: is this right, handle other errors?
				return nil
			}

			m.doRead(b, n, err)
			if err != nil {
				// Further reads won't make any progress, so don't block Close
				// when it's invoked.
				return err
			}
		}
	})

	return m
}

// Close terminates the mux.
func (m *mux) Close() error { return m.eg.Wait() }

// A client is a client handle attached to the mux.
type client struct {
	readC chan<- read
	ctx   context.Context
}

// A read is the result of a read operation. The buffer is shared among multiple
// clients, so clients _must_ only read from the buffer to avoid data races.
type read struct {
	b   []byte
	err error
}

// doRead consumes the results of a Read operation and dispatches them to each
// of the clients attached to the mux.
func (m *mux) doRead(b []byte, n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Make a copy of the reader buffer to dispatch the copy to each client
	// before returning, so the reader can reuse the space.
	buf := make([]byte, n)
	copy(buf, b[:n])

	// remove detaches a given client when its context is canceled.
	// Note that it is legal to modify a map during iteration in Go.
	remove := func(id int) {
		close(m.clients[id].readC)
		delete(m.clients, id)
	}

	for id, c := range m.clients {
		if c.ctx.Err() != nil {
			// Client no longer listening.
			remove(id)
			continue
		}

		// Client is either ready for reading or its context is already
		// canceled.
		//
		// TODO: deal with slow clients by possibly dropping reads.
		select {
		case <-c.ctx.Done():
			// Client no longer listening.
			remove(id)
		case c.readC <- read{b: buf, err: err}:
			// Client is ready to consume the read.
		}
	}
}

// Attach attaches a client to the mux and produces an io.Reader which will
// receive any data read by the mux until the client's context is canceled.
func (m *mux) Attach(ctx context.Context) io.Reader {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Attach the client and give it an auto-incremented unique ID.
	readC := make(chan read)
	m.clients[m.id] = client{
		readC: readC,
		ctx:   ctx,
	}

	m.id++

	return &muxReader{
		ctx:   ctx,
		readC: readC,
	}
}

var _ io.Reader = &muxReader{}

// A muxReader is an io.Reader produced by the mux which consumes data from
// a channel.
type muxReader struct {
	ctx   context.Context
	readC <-chan read
}

// Read implements io.Reader.
func (mr *muxReader) Read(b []byte) (int, error) {
	select {
	case <-mr.ctx.Done():
		// Nothing to do, EOF.
		return 0, io.EOF
	case r := <-mr.readC:
		// Return any read data and errors.
		n := copy(b, r.b)
		return n, r.err
	}
}
