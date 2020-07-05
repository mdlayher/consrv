package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/dolmen-go/contextio"
	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
)

// An sshServer is a wrapped SSH server type.
type sshServer struct {
	s          *ssh.Server
	authorized map[string]struct{}
	devices    map[string]*muxDevice

	mm *metrics
}

// newSSHServer creates an SSH server configured to open connections to the
// input devices.
func newSSHServer(addr, hostKey string, devices map[string]*muxDevice, ids []identity, mm *metrics) (*sshServer, error) {
	srv := &ssh.Server{Addr: addr}
	srv.SetOption(ssh.HostKeyFile(hostKey))

	authorized := make(map[string]struct{})
	for _, id := range ids {
		f := gossh.FingerprintSHA256(id.PublicKey)
		log.Printf("added identity %q: %s", id.Name, f)
		authorized[f] = struct{}{}
	}

	s := &sshServer{
		s:          srv,
		authorized: authorized,
		devices:    devices,
		mm:         mm,
	}

	srv.SetOption(ssh.PublicKeyAuth(s.pubkeyAuth))
	srv.Handle(s.handle)

	return s, nil
}

// Serve begins serving SSH connections.
func (s *sshServer) Serve() error { return s.s.ListenAndServe() }

// pubkeyAuth authenticates users via SSH public key.
func (s *sshServer) pubkeyAuth(ctx ssh.Context, key ssh.PublicKey) bool {
	// Is this client's key authorized for access?
	_, ok := s.authorized[gossh.FingerprintSHA256(key)]

	var action string
	if ok {
		action = "accepted"
	} else {
		action = "rejected"
	}

	s.mm.deviceAuthentications(1.0, action)
	log.Printf("%s: %s public key authentication for %s", ctx.RemoteAddr(), action, gossh.FingerprintSHA256(key))
	return ok
}

// handle handles an opened SSH to serial console session.
func (s *sshServer) handle(session ssh.Session) {
	// Use usernames to map to valid device multiplexers.
	mux, ok := s.devices[session.User()]
	if !ok {
		// No such connection.
		s.mm.deviceUnknownSessions(1.0)
		logf(session, "exiting, unknown connection %q", session.User())
		_ = session.Exit(1)
		return
	}

	done := s.mm.newSession(session.User())
	defer done()

	// Begin proxying between SSH and serial console mux until the SSH
	// connection closes or is broken.
	logf(session, "opened serial connection %q to %s", session.User(), mux.String())

	ctx, cancel := context.WithCancel(session.Context())
	defer cancel()

	// Create a new io.Reader handle from the mux for this client, so it
	// will receive the same output as other clients for the duration of its
	// session.
	r := mux.m.Attach(ctx)

	var eg errgroup.Group
	eg.Go(eofCopy(ctx, mux, session))
	eg.Go(eofCopy(ctx, session, r))

	if err := eg.Wait(); err != nil {
		log.Printf("error proxying SSH/serial for %s: %v", session.RemoteAddr(), err)
	}

	_ = session.Exit(0)
	log.Printf("%s: closed serial connection %q to %s", session.RemoteAddr(), session.User(), mux.String())
}

// eofCopy is a context-aware io.Copy that consumes io.EOF errors and is
// specialized for errgroup use.
func eofCopy(ctx context.Context, w io.Writer, r io.Reader) func() error {
	return func() error {
		_, err := io.Copy(
			contextio.NewWriter(ctx, w),
			contextio.NewReader(ctx, r),
		)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}

		return nil
	}
}

// logf outputs a formatted log message to both stderr and an SSH client.
func logf(s ssh.Session, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Printf("%s: %s", s.RemoteAddr(), msg)
	fmt.Fprintf(s, "consrv> %s\n", msg)
}
