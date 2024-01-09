// Copyright 2023 Berk D. Demir and Matt Layher
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

//go:build gokrazy

package main

import (
	"fmt"
	"os"
	"syscall"
)

const (
	privdropUID = 65534 // conventionally: nobody
	privdropGID = 65534 // conventionally: nogroup
)

func dropPrivileges() (*privilegesInfo, error) {
	dir, err := os.MkdirTemp("/dev/shm", "consrv-chroot-*")
	if err != nil {
		return nil, fmt.Errorf("create chroot directory: %w", err)
	}

	if err := syscall.Chroot(dir); err != nil {
		return nil, fmt.Errorf("chroot %q: %w", dir, err)
	}

	if err := syscall.Setgid(privdropGID); err != nil {
		return nil, fmt.Errorf("setgid: %w", err)
	}

	if err := syscall.Setuid(privdropUID); err != nil {
		return nil, fmt.Errorf("setuid: %w", err)
	}

	return &privilegesInfo{
		Chroot: dir,
		UID:    privdropUID,
		GID:    privdropGID,
	}, nil
}
