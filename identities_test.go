// Copyright 2020 Matt Layher and Michael Stapelberg
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
	"io/ioutil"
	"log"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

const (
	testPublicA = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAII1jZURuUdJ7EwKgTDxKzGSvtEeNeraLS9KeZZMoD0V/ test A"
	testPublicB = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKjKqdP/zqOKQiCUoG95vfW0wR+gZUEACqp3DIAKE6Xj test B"
	testPublicC = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOd5NRST/MRc1BRG5avpdx9O5y7UadHsL4pD8fBTKqoG test C"
)

type idPair struct {
	User string
	Key  gossh.PublicKey
}

func Test_identities(t *testing.T) {
	// Discard all logs.
	ll := log.New(ioutil.Discard, "", 0)

	tests := []struct {
		name        string
		ids         *identities
		allow, deny []idPair
	}{
		{
			name: "empty",
			ids:  newIdentities(nil, ll),
			deny: []idPair{
				{
					User: "foo",
					Key:  mustKey(testPublicA),
				},
				{
					User: "bar",
					Key:  mustKey(testPublicB),
				},
				{
					User: "baz",
					Key:  mustKey(testPublicC),
				},
			},
		},
		{
			name: "global",
			ids: newIdentities(&config{
				Devices: []rawDevice{
					{Name: "foo"},
					{Name: "bar"},
				},
				Identities: []identity{{
					Name:      "test A",
					PublicKey: mustKey(testPublicA),
				}},
			}, ll),
			allow: []idPair{
				{
					User: "foo",
					Key:  mustKey(testPublicA),
				},
				{
					User: "bar",
					Key:  mustKey(testPublicA),
				},
			},
			deny: []idPair{
				{
					User: "foo",
					Key:  mustKey(testPublicB),
				},
				{
					User: "bar",
					Key:  mustKey(testPublicB),
				},
			},
		},
		{
			name: "per-device",
			ids: newIdentities(&config{
				Devices: []rawDevice{
					{
						Name:       "foo",
						Identities: []string{"a"},
					},
					{
						Name:       "bar",
						Identities: []string{"b"},
					},
					{
						Name:       "baz",
						Identities: []string{"a", "b"},
					},
				},
				Identities: []identity{
					{
						Name:      "a",
						PublicKey: mustKey(testPublicA),
					},
					{
						Name:      "b",
						PublicKey: mustKey(testPublicB),
					},
				},
			}, ll),
			allow: []idPair{
				{
					User: "foo",
					Key:  mustKey(testPublicA),
				},
				{
					User: "bar",
					Key:  mustKey(testPublicB),
				},
				{
					User: "baz",
					Key:  mustKey(testPublicA),
				},
				{
					User: "baz",
					Key:  mustKey(testPublicB),
				},
			},
			deny: []idPair{
				{
					User: "foo",
					Key:  mustKey(testPublicB),
				},
				{
					User: "bar",
					Key:  mustKey(testPublicA),
				},
				{
					User: "baz",
					Key:  mustKey(testPublicC),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, id := range tt.allow {
				if !tt.ids.authenticate(id.User, id.Key) {
					t.Fatalf("expected user %q to successfully authenticate", id.User)
				}
			}

			for _, id := range tt.deny {
				if tt.ids.authenticate(id.User, id.Key) {
					t.Fatalf("expected user %q to fail to authenticate", id.User)
				}
			}
		})
	}
}
