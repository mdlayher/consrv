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
		err  error
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
		{
			name: "ambiguous shared serial",
			fs:   testFS(),
			raw: &rawDevice{
				Name:   "quad",
				Serial: "9999",
				Baud:   115200,
			},
			err: errAmbiguousSerial,
		},
		{
			name: "no matching interface",
			fs:   testFS(),
			raw: &rawDevice{
				Name:      "quad",
				Serial:    "9999",
				Interface: intPtr(7),
				Baud:      115200,
			},
		},
		{
			name: "OK shared serial with interface",
			fs:   testFS(),
			raw: &rawDevice{
				Name:      "quad2",
				Serial:    "9999",
				Interface: intPtr(2),
				Baud:      115200,
			},
			want: &serialDevice{
				name:   "quad2",
				device: "/dev/ttyUSB4",
				serial: "9999",
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
			if !tt.ok {
				wantErr := tt.err
				if wantErr == nil {
					wantErr = os.ErrNotExist
				}

				if !errors.Is(err, wantErr) {
					t.Fatalf("expected error %v, but got: %v", wantErr, err)
				}
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

func intPtr(i int) *int { return &i }

func testFS() *fs {
	return &fs{
		glob: func(pattern string) ([]string, error) {
			switch pattern {
			case "/dev/ttyUSB*":
				return []string{
					// A pair of single-port adapters followed by a multi-port
					// adapter whose four ports share one serial number.
					"/dev/ttyUSB0", "/dev/ttyUSB1",
					"/dev/ttyUSB2", "/dev/ttyUSB3", "/dev/ttyUSB4", "/dev/ttyUSB5",
				}, nil
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
			case "/sys/class/tty/ttyUSB2/device/../../serial",
				"/sys/class/tty/ttyUSB3/device/../../serial",
				"/sys/class/tty/ttyUSB4/device/../../serial",
				"/sys/class/tty/ttyUSB5/device/../../serial":
				return []byte("9999"), nil
			case "/sys/class/tty/ttyUSB0/device/../bInterfaceNumber":
				return []byte("00\n"), nil
			case "/sys/class/tty/ttyUSB2/device/../bInterfaceNumber":
				return []byte("00\n"), nil
			case "/sys/class/tty/ttyUSB3/device/../bInterfaceNumber":
				return []byte("01\n"), nil
			case "/sys/class/tty/ttyUSB4/device/../bInterfaceNumber":
				return []byte("02\n"), nil
			case "/sys/class/tty/ttyUSB5/device/../bInterfaceNumber":
				return []byte("03\n"), nil
			case "/sys/class/tty/ttyACM0/device/../serial":
				return []byte("3333"), nil
			case "/sys/class/tty/ttyACM0/device/bInterfaceNumber":
				// Pretend this device doesn't expose an interface number.
				return nil, os.ErrNotExist
			default:
				return nil, fmt.Errorf("readFile: unhandled file: %q", file)
			}
		},
		openPort: func(_ *serial.Config) (io.ReadWriteCloser, error) {
			return nil, nil
		},
	}
}
