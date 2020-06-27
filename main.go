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
	"sync"

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
	// Open the serial port device.
	openSerial := func(name string) openFunc {
		return func() (io.ReadWriteCloser, error) {
			return serial.OpenPort(&serial.Config{
				Name: name,
				Baud: 115200,
			})
		}
	}

	dm := newDeviceMap(map[string]openFunc{
		"server":  openSerial("/dev/ttyUSB0"),
		"desktop": openSerial("/dev/ttyUSB1"),
	})

	// Start the SSH server and configure the handler.
	srv, err := newSSHServer(":2222", "/perm/consrv/host_key", "/perm/consrv/authorized_keys")
	if err != nil {
		log.Fatalf("failed to create SSH server: %v", err)
	}

	srv.Handle(func(s ssh.Session) {
		// Use usernames to map to valid serial devices.
		port, err := dm.Open(s.User())
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				logf(s, "invalid connection %q, closing connection", s.User())
			} else {
				logf(s, "failed to open connection %q: %v", s.User(), err)
			}

			_ = s.Exit(1)
			return
		}
		defer port.Close()

		// Begin proxying between SSH and serial console until the SSH
		// connection closes or is broken.
		logf(s, "opened serial connection %q", s.User())

		var eg errgroup.Group
		eg.Go(eofCopy(port, s))
		eg.Go(eofCopy(s, port))

		if err := eg.Wait(); err != nil {
			log.Printf("error proxying SSH/serial for %s: %v", s.RemoteAddr(), err)
		}
		_ = s.Close()

		log.Printf("%s: closed serial connection %q", s.RemoteAddr(), s.User())
	})

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("failed to serve SSH: %v", err)
	}
}

type openFunc func() (io.ReadWriteCloser, error)

type deviceMap struct {
	mu   sync.Mutex
	m    map[string]openFunc
	prev map[string]io.Closer
}

func newDeviceMap(m map[string]openFunc) *deviceMap {
	return &deviceMap{
		m:    m,
		prev: make(map[string]io.Closer),
	}
}

func (dm *deviceMap) Open(user string) (io.ReadWriteCloser, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	fn, ok := dm.m[user]
	if !ok {
		return nil, os.ErrNotExist
	}

	rwc, err := fn()
	if err != nil {
		return nil, err
	}

	if c, ok := dm.prev[user]; ok {
		_ = c.Close()
	}
	dm.prev[user] = rwc

	return rwc, nil
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

// eofCopy is like io.Copy but it consumes io.EOF errors and is specialized for
// errgroup use.
func eofCopy(w io.Writer, r io.Reader) func() error {
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
