// Command consrv is a basic SSH to serial console bridge server for gokrazy.org
// appliances.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/gliderlabs/ssh"
	"github.com/tarm/serial"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
)

// WIP WIP WIP, there's a lot more to do!
//
// TODO:
//  - Prometheus metrics
//  - support for multiple serial devices with different usernames
//  - remove hardcoded devices/paths for non-gokrazy machines
//  - separate users log into separate serial ports
//  - capture and inspect/alert on kernel panics
//  - magic sysrq support
//  - signal handler to block until all connections close?

func main() {
	// TODO: parse from config file, open serial console on demand.
	const device = "/dev/ttyUSB0"
	hosts := map[string]string{
		"server": device,
	}

	// Open the serial port device.

	port, err := serial.OpenPort(&serial.Config{
		Name: device,
		Baud: 115200,
	})
	if err != nil {
		log.Fatalf("failed to open serial port: %v", err)
	}
	defer port.Close()

	// Start the SSH server and configure the handler.
	srv, err := newSSHServer(":2222", "/perm/consrv/host_key", "/perm/consrv/authorized_keys")
	if err != nil {
		log.Fatalf("failed to create SSH server: %v", err)
	}

	srv.Handle(func(s ssh.Session) {
		// Use usernames to map to valid serial devices.
		if _, ok := hosts[s.User()]; !ok {
			logf(s, "invalid connection %q, closing connection", s.User())
			_ = s.Exit(1)
			return
		}

		// Begin proxying between SSH and serial console until the SSH
		// connection closes or is broken.
		logf(s, "opened serial connection %q to %s", s.User(), device)

		var eg errgroup.Group
		eg.Go(copy(port, s))
		eg.Go(copy(s, port))

		if err := eg.Wait(); err != nil {
			log.Printf("error proxying SSH/serial for %s: %v", s.RemoteAddr(), err)
		}
		_ = s.Close()

		log.Printf("%s: closed connection %q to %s", s.RemoteAddr(), s.User(), device)
	})

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("failed to serve SSH: %v", err)
	}
}

// logf outputs a formatted log message to both stderr and an SSH client.
func logf(s ssh.Session, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Printf("%s: %s", s.RemoteAddr(), msg)
	fmt.Fprintf(s, "consrv> %s\n", msg)
}

// newSSHServer creates an SSH server which will bind to the specified address
// and use the input host key and authorized key files.
func newSSHServer(addr, hostKey, authorizedKeys string) (*ssh.Server, error) {
	auth, err := os.Open(authorizedKeys)
	if err != nil {
		log.Fatalf("failed to open authorized keys file, err: %v", err)
	}
	defer auth.Close()

	authorized, err := parseAuthorizedKeys(auth)
	if err != nil {
		log.Fatalf("failed to parse authorized keys: %v", err)
	}
	_ = auth.Close()

	srv := &ssh.Server{Addr: addr}
	srv.SetOption(ssh.HostKeyFile(hostKey))

	srv.SetOption(ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
		// Is this client's key authorized for access?
		_, ok := authorized[gossh.FingerprintSHA256(key)]

		action := "rejected"
		if ok {
			action = "accepted"
		}

		log.Printf("%s: %s public key authentication for %s", ctx.RemoteAddr(), action, gossh.FingerprintSHA256(key))
		return ok
	}))

	return srv, nil
}

// copy is like io.Copy but it consumes io.EOF errors and is specialized for
// errgroup use.
func copy(w io.Writer, r io.Reader) func() error {
	return func() error {
		if _, err := io.Copy(w, r); err != nil && !errors.Is(err, io.EOF) {
			return err
		}

		return nil
	}
}

// parseAuthorizedKeys consumes an input stream and parses a set of all
// authorized keys for SSH access.
func parseAuthorizedKeys(r io.Reader) (map[string]struct{}, error) {
	authorized := make(map[string]struct{})
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			// Skip empty lines and comments.
			continue
		}

		key, _, _, _, err := ssh.ParseAuthorizedKey(s.Bytes())
		if err != nil {
			return nil, fmt.Errorf("failed to parse %q: %v", line, err)
		}

		authorized[gossh.FingerprintSHA256(key)] = struct{}{}
	}

	return authorized, nil
}
