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
	"log"
	"os"
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
			name: "OK devices USB serial",
			fs:   testFS(),
			raw: &rawDevice{
				Name:   "foo",
				Serial: "1111",
				Baud:   115200,
			},
			want: &serialDevice{
				name:   "foo",
				device: "/dev/ttyUSB0",
				serial: "1111",
				baud:   115200,
			},
			ok: true,
		},
		{
			name: "OK devices ACM serial",
			fs:   testFS(),
			raw: &rawDevice{
				Name:   "bar",
				Serial: "3333",
				Baud:   115200,
			},
			want: &serialDevice{
				name:   "bar",
				device: "/dev/ttyACM0",
				serial: "3333",
				baud:   115200,
			},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fs.init(log.Default()); err != nil {
				t.Fatalf("failed to init fs: %v", err)
			}

			d, err := tt.fs.openSerial(tt.raw, nil, nil)
			if tt.ok && err != nil {
				t.Fatalf("failed to open serial: %v", err)
			}
			if !tt.ok && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected is not exist, but got: %v", err)
			}

			if diff := cmp.Diff(tt.want, d, cmp.Comparer(devicesEqual)); diff != "" {
				t.Fatalf("unexpected device (-want +got):\n%s", diff)
			}
		})
	}
}

func devicesEqual(x, y device) bool {
	if x == nil || y == nil {
		return false
	}

	return x.String() == y.String()
}

func testFS() *fs {
	return &fs{
		glob: func(pattern string) ([]string, error) {
			switch pattern {
			case "/dev/ttyUSB*":
				return []string{"/dev/ttyUSB0", "/dev/ttyUSB1"}, nil
			case "/dev/ttyACM*":
				return []string{"/dev/ttyACM0"}, nil
			default:
				return nil, fmt.Errorf("glob: unhandled pattern: %q", pattern)
			}
		},
		readFile: func(file string) ([]byte, error) {
			switch file {
			case "/sys/class/tty/ttyUSB0/device/../../serial":
				return []byte("1111"), nil
			case "/sys/class/tty/ttyUSB1/device/../../serial":
				// Pretend this device doesn't have a serial number.
				return nil, os.ErrNotExist
			case "/sys/class/tty/ttyACM0/device/../serial":
				return []byte("3333"), nil
			default:
				return nil, fmt.Errorf("readFile: unhandled file: %q", file)
			}
		},
		openPort: func(_ *serial.Config) (io.ReadWriteCloser, error) {
			return nil, nil
		},
	}
}
