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
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/nettest"
	"golang.org/x/sync/errgroup"
)

// TODO: test for authentication failure.

// ed25519 host and client authentication keypairs, which are only used in tests
// and should never be used elsewhere.
const (
	testHostPrivate = `
-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBWTt+ou9iu446ncozod5HjB+osDzsVHJY9WW/JTUDQ8wAAAJC8+iHfvPoh
3wAAAAtzc2gtZWQyNTUxOQAAACBWTt+ou9iu446ncozod5HjB+osDzsVHJY9WW/JTUDQ8w
AAAEDa8yEuldTAKU662fVbn+WwNorOLmoJuBBhDUaNJER0E1ZO36i72K7jjqdyjOh3keMH
6iwPOxUclj1Zb8lNQNDzAAAAC21hdHRAbmVyci0zAQI=
-----END OPENSSH PRIVATE KEY-----
`

	testHostPublic = `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFZO36i72K7jjqdyjOh3keMH6iwPOxUclj1Zb8lNQNDz test host`

	testClientPrivate = `
-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACAY/MTzbnjP3+nTUe+q2sX+s+rhDM2Cut11LNYicfRFMQAAAJA0smuQNLJr
kAAAAAtzc2gtZWQyNTUxOQAAACAY/MTzbnjP3+nTUe+q2sX+s+rhDM2Cut11LNYicfRFMQ
AAAED/FAqSFGQ30rPEhV/hevaH31kIcW/ZeNVpN8FGNVJQNRj8xPNueM/f6dNR76raxf6z
6uEMzYK63XUs1iJx9EUxAAAAC21hdHRAbmVyci0zAQI=
-----END OPENSSH PRIVATE KEY-----
`

	testClientPublic = `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBj8xPNueM/f6dNR76raxf6z6uEMzYK63XUs1iJx9EUx test client`
)

func TestSSHUnknownDevice(t *testing.T) {
	// Open a session with a server that has no devices configured, and thus
	// cannot open a valid consrv session.
	s := testSSH(t, "test", nil)

	var serr *ssh.ExitError
	out, err := s.CombinedOutput("")
	if !errors.As(err, &serr) {
		t.Fatalf("session did not return SSH exit error: %v", err)
	}

	if diff := cmp.Diff(1, serr.ExitStatus()); diff != "" {
		t.Fatalf("unexpected SSH exit status (-want +got):\n%s", diff)
	}

	const msg = `consrv> exiting, unknown connection "test"` + "\n"
	if diff := cmp.Diff(msg, string(out)); diff != "" {
		t.Fatalf("unexpected SSH output (-want +got):\n%s", diff)
	}
}

func TestSSHSuccess(t *testing.T) {
	// Connect to a device which will notify us when it receives data from the
	// SSH session, and allow us to inspect the written bytes later.
	d := &testDevice{writeC: make(chan struct{})}
	s := testSSH(t, "test", map[string]*muxDevice{
		"test": newMuxDevice(d),
	})

	const msg = "hello world"
	s.Stdin = strings.NewReader(msg)

	var buf bytes.Buffer
	s.Stdout = &buf

	if err := s.Start(""); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}

	// Wait for the device to receive a Write and forcibly terminate the session
	// to stop the bidirectional copy between SSH and device. This will generate
	// an error, but that's okay because the actual application would never
	// terminate unless the user terminates the SSH session anyway.
	<-d.writeC
	if err := s.Close(); err != nil {
		t.Fatalf("failed to close session: %v", err)
	}

	var serr *ssh.ExitError
	if err := s.Wait(); !errors.As(err, &serr) {
		t.Fatalf("session did not return SSH exit error: %v", err)
	}

	// Verify that stdin data was written to the device, and that the device
	// also presented a successful login banner.
	if diff := cmp.Diff(msg, string(d.write)); diff != "" {
		t.Fatalf("unexpected device write data (-want +got):\n%s", diff)
	}

	const banner = `consrv> opened serial connection test` + "\n"
	if diff := cmp.Diff(banner, buf.String()); diff != "" {
		t.Fatalf("unexpected SSH banner (-want +got):\n%s", diff)
	}
}

var _ device = &testDevice{}

type testDevice struct {
	read, write []byte
	writeC      chan struct{}
}

func (d *testDevice) Read(b []byte) (int, error) {
	// EOF after a single read.
	n := copy(b, d.read)
	return n, io.EOF
}

func (d *testDevice) Write(b []byte) (int, error) {
	d.write = append(d.write, b...)
	close(d.writeC)
	return len(b), nil
}

func (d *testDevice) Close() error { return nil }

func (d *testDevice) String() string { return "test" }

// testSSH creates a test SSH session pointed at an ephemeral server.
func testSSH(t *testing.T, user string, devices map[string]*muxDevice) *ssh.Session {
	t.Helper()

	// Set up a local listener on an ephemeral port for the SSH server.
	l, err := nettest.NewLocalListener("tcp")
	if err != nil {
		t.Fatalf("failed to create local listener: %v", err)
	}
	t.Cleanup(func() {
		_ = l.Close()
	})

	ll := log.New(os.Stderr, "", 0)

	// Allow authentication from a single predefined keypair.
	ids := newIdentities(&config{
		Identities: []identity{{
			Name:      "test",
			PublicKey: mustKey(testClientPublic),
		}},
	}, ll)

	srv, err := newSSHServer(
		[]byte(strings.TrimSpace(testHostPrivate)),
		devices,
		ids,
		ll,
		newMetrics(nil),
	)
	if err != nil {
		t.Fatalf("failed to create SSH server: %v", err)
	}

	// Begin serving SSH until the listener is forcibly closed in the cleanup
	// phase of the test.
	var eg errgroup.Group
	eg.Go(func() error {
		if err := srv.Serve(l); err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return nil
			}

			return fmt.Errorf("failed to serve SSH: %v", err)
		}

		return nil
	})

	// Create a client which is configured to accept the server's host key and
	// also use public key authentication.
	priv, err := ssh.ParsePrivateKey([]byte(strings.TrimSpace(testClientPrivate)))
	if err != nil {
		t.Fatalf("failed to parse private key: %v", err)
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(priv)},
		HostKeyCallback: ssh.FixedHostKey(mustKey(testHostPublic)),
	}

	// Dial the server's address and open a session for the remainder of the
	// test run.
	c, err := ssh.Dial("tcp", l.Addr().String(), cfg)
	if err != nil {
		t.Fatalf("failed to dial SSH: %v", err)
	}

	s, err := c.NewSession()
	if err != nil {
		t.Fatalf("failed to create SSH session: %v", err)
	}

	t.Cleanup(func() {
		// Clean up all of the temporary connections and verify the test can
		// properly halt the server.
		_ = s.Close()
		_ = c.Close()
		_ = l.Close()

		if err := eg.Wait(); err != nil {
			t.Fatalf("failed to wait: %v", err)
		}
	})

	return s
}
