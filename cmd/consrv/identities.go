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
	"log"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// An identities configures a set of identities which may be used for either
// per-device or global authentication.
type identities struct {
	perDevice map[string]set[string]
	global    set[string]

	// Maps fingerprint back to friendly name for logs.
	toName map[string]string
}

// A set is a unique set of T.
type set[T comparable] map[T]struct{}

// add adds t to the set.
func (s set[T]) add(t T) { s[t] = struct{}{} }

// has returns if t is in the set.
func (s set[T]) has(t T) bool {
	_, ok := s[t]
	return ok
}

// newIdentities creates an identities map from configuration.
func newIdentities(cfg *config, ll *log.Logger) *identities {
	// Set up relationships between devices and the identities which are
	// authorized to access them.
	ids := identities{
		perDevice: make(map[string]set[string]),
		global:    make(set[string]),

		toName: make(map[string]string),
	}

	if cfg == nil {
		return &ids
	}

	// Configure global identities which can access all devices unless
	// device-specific identities are configured.
	known := make(map[string]string)
	for _, id := range cfg.Identities {
		f := gossh.FingerprintSHA256(id.PublicKey)
		ll.Printf("added identity %q: %s", id.Name, f)

		known[id.Name] = f
		ids.global.add(f)
		ids.toName[f] = id.Name
	}

	for _, d := range cfg.Devices {
		if len(d.Identities) == 0 {
			// Let the user know that any configured identity will be able to
			// access this device.
			ll.Printf("warning: all identities allowed for device %q", d.Name)
			continue
		}

		if ids.perDevice[d.Name] == nil {
			ids.perDevice[d.Name] = make(set[string])
		}

		for _, id := range d.Identities {
			f, ok := known[id]
			if !ok {
				// We've already validated the configuration upon parsing so any
				// unknown identity in the device configuration at this point is
				// a clear programming error.
				panic("consrv: invalid device configuration")
			}

			// This device will only accept authentication for a specific set
			// of identities.
			ll.Printf("identity %q configured for device %q", id, d.Name)
			ids.perDevice[d.Name].add(f)
		}
	}

	return &ids
}

// authenticate determines if the specified user and public key combination are
// able to authenticate against a device's configuration. If so, the friendly
// name of the identity is also returned for logging.
func (ids *identities) authenticate(user string, key ssh.PublicKey) (string, bool) {
	f := gossh.FingerprintSHA256(key)

	if pd, ok := ids.perDevice[user]; ok {
		// This device only allows specific identities.
		if !pd.has(f) {
			return "", false
		}
	} else {
		// All identities are permitted.
		if !ids.global.has(f) {
			return "", false
		}
	}

	return ids.toName[f], true
}
