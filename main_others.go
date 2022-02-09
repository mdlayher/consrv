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

//go:build !gokrazy
// +build !gokrazy

package main

import "flag"

// filePaths provides flag configured paths for non-gokrazy systems.
func filePaths() (string, string) {
	var (
		c = flag.String("c", "consrv.toml", "path to consrv.toml configuration file")
		k = flag.String("k", "host_key", "path to OpenSSH format host key file")
	)

	flag.Parse()

	return *c, *k
}
