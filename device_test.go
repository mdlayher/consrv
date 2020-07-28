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
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/tarm/serial"
)

func Test_fs_openSerial(t *testing.T) {
	tests := []struct {
		name string
		fs   *fs
		raw  *rawDevice
		want device
		ok   bool
	}{
		{
			name: "no input device",
			fs: &fs{
				openPort: func(_ *serial.Config) (io.ReadWriteCloser, error) {
					return nil, os.ErrNotExist
				},
			},
			raw: &rawDevice{},
		},
		{
			name: "no matching serial",
			fs: &fs{
				glob: func(_ string) ([]string, error) {
					return []string{"/dev/ttyUSB0"}, nil
				},
				readFile: func(_ string) ([]byte, error) {
					return nil, os.ErrNotExist
				},
			},
			raw: &rawDevice{
				Serial: "DEADBEEF",
			},
		},
		{
			name: "OK device path",
			fs: &fs{
				openPort: func(_ *serial.Config) (io.ReadWriteCloser, error) {
					return nil, nil
				},
			},
			raw: &rawDevice{
				Name:   "foo",
				Device: "/dev/ttyUSB0",
				Baud:   115200,
			},
			want: &serialDevice{
				name:   "foo",
				device: "/dev/ttyUSB0",
				baud:   115200,
			},
			ok: true,
		},
		{
			name: "OK device path",
			fs: &fs{
				openPort: func(_ *serial.Config) (io.ReadWriteCloser, error) {
					return nil, nil
				},
			},
			raw: &rawDevice{
				Name:   "foo",
				Device: "/dev/ttyUSB0",
				Baud:   115200,
			},
			want: &serialDevice{
				name:   "foo",
				device: "/dev/ttyUSB0",
				baud:   115200,
			},
			ok: true,
		},
		{
			name: "OK device serial",
			fs: &fs{
				glob: func(_ string) ([]string, error) {
					return []string{"/dev/ttyUSB0", "/dev/ttyUSB1"}, nil
				},
				readFile: func(file string) ([]byte, error) {
					if strings.Contains(file, "ttyUSB0") {
						return []byte("DEADBEEF"), nil
					}

					return nil, os.ErrNotExist
				},
				openPort: func(_ *serial.Config) (io.ReadWriteCloser, error) {
					return nil, nil
				},
			},
			raw: &rawDevice{
				Name:   "foo",
				Serial: "DEADBEEF",
				Baud:   115200,
			},
			want: &serialDevice{
				name:   "foo",
				device: "/dev/ttyUSB0",
				serial: "DEADBEEF",
				baud:   115200,
			},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := tt.fs.openSerial(tt.raw, nil, nil)
			if tt.ok && err != nil {
				t.Fatalf("failed to open serial: %v", err)
			}
			if !tt.ok && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected is not exist, but got: %v", err)
			}

			if diff := cmp.Diff(tt.want, d, cmp.Comparer(compareDevices)); diff != "" {
				t.Fatalf("unexpected device (-want +got):\n%s", diff)
			}
		})
	}
}

func compareDevices(x, y device) bool {
	if x == nil || y == nil {
		return false
	}

	return x.String() == y.String()
}
