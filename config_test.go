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
	"fmt"
	"strings"
	"testing"

	"github.com/gliderlabs/ssh"
	"github.com/google/go-cmp/cmp"
)

func Test_parseConfig(t *testing.T) {
	tests := []struct {
		name string
		s    string
		c    *config
		ok   bool
	}{
		{
			name: "bad TOML",
			s:    "xxx",
		},
		{
			name: "bad keys",
			s: `
			[bad]
			[[bad.bad]]
			`,
		},
		{
			name: "bad no devices",
			s: `
			[[identities]]
			`,
		},
		{
			name: "bad no identities",
			s: `
			[[devices]]
			`,
		},
		{
			name: "bad identity name",
			s: `
			[[devices]]
			name = "foo"
			device = "/dev/ttyUSB0"
			baud = 115200

			[[identities]]
			name = ""
			`,
		},
		{
			name: "bad identity public key",
			s: `
			[[devices]]
			name = "foo"
			device = "/dev/ttyUSB0"
			baud = 115200

			[[identities]]
			name = "bar"
			public_key = "foo"
			`,
		},
		{
			name: "bad device name",
			s: `
			[[devices]]
			name = ""

			[[identities]]
			name = "ed25519"
			public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ6PAHCvJTosPqBppE6lmjjRt9Qlcisqx+DXt7jIbLba test ed25519"
			`,
		},
		{
			name: "bad device path",
			s: `
			[[devices]]
			name = "foo"
			device = ""
			baud = 115200

			[[identities]]
			name = "ed25519"
			public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ6PAHCvJTosPqBppE6lmjjRt9Qlcisqx+DXt7jIbLba test ed25519"
			`,
		},
		{
			name: "bad device serial",
			s: `
			[[devices]]
			name = "foo"
			serial = ""
			baud = 115200

			[[identities]]
			name = "ed25519"
			public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ6PAHCvJTosPqBppE6lmjjRt9Qlcisqx+DXt7jIbLba test ed25519"
			`,
		},
		{
			name: "bad device baud rate",
			s: `
			[[devices]]
			name = "foo"
			device = "/dev/ttyUSB0"
			baud = 0

			[[identities]]
			name = "ed25519"
			public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ6PAHCvJTosPqBppE6lmjjRt9Qlcisqx+DXt7jIbLba test ed25519"
			`,
		},
		{
			name: "bad device identity",
			s: `
			[[devices]]
			name = "server"
			device = "/dev/ttyUSB0"
			baud = 115200
			identities = ["bad"]

			[[identities]]
			name = "ed25519"
			public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ6PAHCvJTosPqBppE6lmjjRt9Qlcisqx+DXt7jIbLba test ed25519"
			`,
		},
		{
			name: "OK",
			s: `
			[[devices]]
			name = "server"
			device = "/dev/ttyUSB0"
			baud = 115200
			identities = ["ed25519"]

			[[devices]]
			name = "desktop"
			serial = "DEADBEEF"
			baud = 115200

			[[identities]]
			name = "ed25519"
			public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ6PAHCvJTosPqBppE6lmjjRt9Qlcisqx+DXt7jIbLba test ed25519"

			[[identities]]
			name = "rsa"
			public_key = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDDkvg9+NTySctVaMkbZGwTRIUiQSo4crGWQPeFTi/XM3KhcUY+WduwHChJX1h03/DKJps8wtHUn3LmUKFR4BoJEgt8Od+L6ey5sev4lvPa2wDc5HJfervgCnVt9aomdFqeZUe6g4BDdPLUGbzT3T+A+08ocXy/eVv9Kke7Ka6GslJQQ5TBjW0AbPhxu6QmoZDb0tiWf9CwyVpiox5+vW7E+O6U1QOKT45Ellc2smHSAcI1gUDborS0GhFSso9SagMxcWNbZf8920DeaLs5tb8uwKfWKqHJfkY+VK3QuufpWZM3BJTPa0PePd75NRra2BOV4LDwGlLrZjOCULlYawDlDOIm6rpC3QV7juHTFWjS8ImvbsyEWZSE9N6klDMc23Zl9vhqJcG4U9LVAv2QMcr8aXBnmSo49rkd7/H6yHZgWqmrAijloZkiwsTbofT+lQx3JLEagk1rd8rmCp4F7WeUShvvmTq0tyPDutIhd1TXwLB0gyFObCDgb3CrXPtsACc= test RSA"
			`,
			c: &config{
				Devices: []rawDevice{
					{
						Name:       "server",
						Device:     "/dev/ttyUSB0",
						Baud:       115200,
						Identities: []string{"ed25519"},
					},
					{
						Name:   "desktop",
						Serial: "DEADBEEF",
						Baud:   115200,
					},
				},
				Identities: []identity{
					{
						Name:      "ed25519",
						PublicKey: mustKey("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ6PAHCvJTosPqBppE6lmjjRt9Qlcisqx+DXt7jIbLba test ed25519"),
					},
					{
						Name:      "rsa",
						PublicKey: mustKey("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDDkvg9+NTySctVaMkbZGwTRIUiQSo4crGWQPeFTi/XM3KhcUY+WduwHChJX1h03/DKJps8wtHUn3LmUKFR4BoJEgt8Od+L6ey5sev4lvPa2wDc5HJfervgCnVt9aomdFqeZUe6g4BDdPLUGbzT3T+A+08ocXy/eVv9Kke7Ka6GslJQQ5TBjW0AbPhxu6QmoZDb0tiWf9CwyVpiox5+vW7E+O6U1QOKT45Ellc2smHSAcI1gUDborS0GhFSso9SagMxcWNbZf8920DeaLs5tb8uwKfWKqHJfkY+VK3QuufpWZM3BJTPa0PePd75NRra2BOV4LDwGlLrZjOCULlYawDlDOIm6rpC3QV7juHTFWjS8ImvbsyEWZSE9N6klDMc23Zl9vhqJcG4U9LVAv2QMcr8aXBnmSo49rkd7/H6yHZgWqmrAijloZkiwsTbofT+lQx3JLEagk1rd8rmCp4F7WeUShvvmTq0tyPDutIhd1TXwLB0gyFObCDgb3CrXPtsACc= test RSA"),
					},
				},
			},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parseConfig(strings.NewReader(tt.s))
			if tt.ok && err != nil {
				t.Fatalf("failed to parse config: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected an error, but none occurred")
			}
			if err != nil {
				t.Logf("err: %v", err)
				return
			}

			if diff := cmp.Diff(tt.c, c, cmp.Comparer(keysEqual)); diff != "" {
				t.Fatalf("unexpected config (-want +got):\n%s", diff)
			}
		})
	}
}

func keysEqual(x, y ssh.PublicKey) bool { return ssh.KeysEqual(x, y) }

func mustKey(s string) ssh.PublicKey {
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(s))
	if err != nil {
		panicf("failed to parse identity public key %q: %v", s, err)
	}

	return k
}

func panicf(format string, a ...interface{}) {
	panic(fmt.Sprintf(format, a...))
}
