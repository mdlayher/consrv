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
	"errors"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/dolmen-go/contextio"
	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
)

// An sshServer is a wrapped SSH server type.
type sshServer struct {
	s       *ssh.Server
	devices map[string]*muxDevice
	ids     *identities

	ll *log.Logger
	mm *metrics
}

// newSSHServer creates an SSH server configured to open connections to the
// input devices.
func newSSHServer(hostKey []byte, devices map[string]*muxDevice, ids *identities, ll *log.Logger, mm *metrics) (*sshServer, error) {
	srv := &ssh.Server{}
	srv.SetOption(ssh.HostKeyPEM(hostKey))

	s := &sshServer{
		s:       srv,
		devices: devices,
		ids:     ids,

		ll: ll,
		mm: mm,
	}

	srv.PublicKeyHandler = s.pubkeyAuth
	srv.Handler = s.handle

	return s, nil
}

// Serve begins serving SSH connections on l.
func (s *sshServer) Serve(l net.Listener) error { return s.s.Serve(l) }

// pubkeyAuth authenticates users via SSH public key.
func (s *sshServer) pubkeyAuth(ctx ssh.Context, key ssh.PublicKey) bool {
	name, ok := s.ids.authenticate(ctx.User(), key)

	var id, action string
	if ok {
		// Success, log the friendly name of the public key identity.
		id = name
		action = "accepted"
	} else {
		// Failure, log the fingerprint of the unknown public key identity.
		id = gossh.FingerprintSHA256(key)
		action = "rejected"
	}

	s.mm.deviceAuthentications(1.0, action)

	// We can't use the logf helper because we don't want to print this
	// information to the SSH session.
	s.ll.Printf("%s: %s public key authentication for %q", addrString(ctx.RemoteAddr()), action, id)
	return ok
}

// handle handles an opened SSH to serial console session.
func (s *sshServer) handle(session ssh.Session) {
	// Use usernames to map to valid device multiplexers.
	mux, ok := s.devices[session.User()]
	if !ok {
		// No such connection.
		s.mm.deviceUnknownSessions(1.0)
		s.logf(session, "exiting, unknown connection %q", session.User())
		_ = session.Exit(1)
		return
	}

	done := s.mm.newSession(session.User())
	defer done()

	// Begin proxying between SSH and serial console mux until the SSH
	// connection closes or is broken.
	s.logf(session, "opened serial connection %s", mux.String())

	ctx, cancel := context.WithCancel(session.Context())
	defer cancel()

	// Create a new io.Reader handle from the mux for this client, so it will
	// receive the same output as other clients for the duration of its session.
	//
	// We can't use the logf helper beyond this point because we don't want to
	// print any further information to the SSH session.
	r := mux.m.Attach(ctx)

	var eg errgroup.Group
	eg.Go(eofCopy(ctx, mux, session))
	eg.Go(eofCopy(ctx, session, r))

	if err := eg.Wait(); err != nil {
		// TODO(mdlayher): re-initialize serial on error? I've had to restart
		// consrv once due to I/O errors on one device.
		s.ll.Printf("%s: error proxying SSH/serial: %v", addrString(session.RemoteAddr()), err)
	}

	_ = session.Exit(0)
	s.ll.Printf("%s: closed serial connection %s", addrString(session.RemoteAddr()), mux)
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
func (s *sshServer) logf(session ssh.Session, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	s.ll.Printf("%s: %s", addrString(session.RemoteAddr()), msg)
	fmt.Fprintf(session, "consrv> %s\n", msg)
}

// addrString prints a friendly string for a net.Addr.
func addrString(addr net.Addr) string {
	// For TCP connections just show the IP address in logs. Otherwise print the
	// entire remote address.
	if ta, ok := addr.(*net.TCPAddr); ok {
		return ta.IP.String()
	}

	return addr.String()
}
