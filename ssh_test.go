package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/nettest"
	"golang.org/x/sync/errgroup"
)

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
		log.Fatalf("session did not return SSH exit error: %v", err)
	}

	if diff := cmp.Diff(1, serr.ExitStatus()); diff != "" {
		t.Fatalf("unexpected SSH exit status (-want +got):\n%s", diff)
	}

	const msg = `consrv> exiting, unknown connection "test"` + "\n"
	if diff := cmp.Diff(msg, string(out)); diff != "" {
		t.Fatalf("unexpected SSH output (-want +got):\n%s", diff)
	}
}

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

	// Allow authentication from a single predefined keypair.
	ids := []identity{{
		Name:      "test",
		PublicKey: mustKey(testClientPublic),
	}}

	srv, err := newSSHServer(
		[]byte(strings.TrimSpace(testHostPrivate)),
		devices,
		ids,
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
