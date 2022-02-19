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
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/BurntSushi/toml"
	"github.com/gliderlabs/ssh"
)

// TODO: allowing linking specific identities with specific devices.

// A config is the consrv configuration.
type config struct {
	Server     server
	Devices    []rawDevice
	Identities []identity
	Debug      debug
}

// server contains consrv SSH server configuration.
type server struct {
	Address string `toml:"address"`
}

// An identity is a processed identity configuration.
type identity struct {
	Name      string
	PublicKey ssh.PublicKey
}

// file is the raw top-level configuration file representation.
type file struct {
	Server     server        `toml:"server"`
	Devices    []rawDevice   `toml:"devices"`
	Identities []rawIdentity `toml:"identities"`
	Debug      debug         `toml:"debug"`
}

// A rawDevice is a raw device configuration.
type rawDevice struct {
	Name       string   `toml:"name"`
	Device     string   `toml:"device"`
	Serial     string   `toml:"serial"`
	Baud       int      `toml:"baud"`
	Identities []string `toml:"identities"`
}

// A rawIdentity is a raw identity configuration.
type rawIdentity struct {
	Name      string `toml:"name"`
	PublicKey string `toml:"public_key"`
}

// debug contains consrv debug configuration.
type debug struct {
	Address    string `toml:"address"`
	Prometheus bool   `toml:"prometheus"`
	PProf      bool   `toml:"pprof"`
}

// defaultSSH is the SSH server address used if no server address is specified.
const defaultSSH = ":2222"

// parseConfig parses a TOML configuration file into a config.
func parseConfig(r io.Reader) (*config, error) {
	var f file
	md, err := toml.NewDecoder(r).Decode(&f)
	if err != nil {
		return nil, err
	}
	if u := md.Undecoded(); len(u) > 0 {
		return nil, fmt.Errorf("unrecognized configuration keys: %s", u)
	}

	// Must configure at least one device and identity.
	if len(f.Devices) == 0 {
		return nil, errors.New("no configured devices")
	}
	if len(f.Identities) == 0 {
		return nil, errors.New("no configured identities")
	}

	if f.Server.Address != "" {
		// Validate the configured SSH server address.
		if _, err := net.ResolveTCPAddr("tcp", f.Server.Address); err != nil {
			return nil, fmt.Errorf("failed to parse SSH server address: %v", err)
		}
	} else {
		// Use the default.
		f.Server.Address = defaultSSH
	}

	// Track the identities found so they can be matched against devices which
	// only allow access from a specific identity.
	validIDs := make(map[string]struct{})
	ids := make([]identity, 0, len(f.Identities))

	// Identities must have each field set, and have a valid public key.
	for _, id := range f.Identities {
		if id.Name == "" {
			return nil, errors.New("identity must have a name")
		}

		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(id.PublicKey))
		if err != nil {
			return nil, fmt.Errorf("failed to parse identity public key %q: %v", id.PublicKey, err)
		}

		validIDs[id.Name] = struct{}{}
		ids = append(ids, identity{
			Name:      id.Name,
			PublicKey: key,
		})
	}

	// Devices must have each field set.
	for _, d := range f.Devices {
		if d.Name == "" {
			return nil, errors.New("device must have a name")
		}

		if d.Baud == 0 {
			return nil, fmt.Errorf("device %q must have a baud rate set", d.Name)
		}

		// Must have at least one identifying field present.
		if d.Device == "" && d.Serial == "" {
			return nil, fmt.Errorf("device %q must have a device path or serial", d.Name)
		}

		// If the device has identities configured, those identities must exist.
		for _, id := range d.Identities {
			if _, ok := validIDs[id]; !ok {
				return nil, fmt.Errorf("device %q is configured with unknown identity %q", d.Name, id)
			}
		}
	}

	// Validate debug configuration if set.
	if f.Debug.Address != "" {
		if _, err := net.ResolveTCPAddr("tcp", f.Debug.Address); err != nil {
			return nil, fmt.Errorf("failed to parse debug HTTP server address: %v", err)
		}
	}

	return &config{
		Server:     f.Server,
		Devices:    f.Devices,
		Identities: ids,
		Debug:      f.Debug,
	}, nil
}
