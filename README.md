# consrv [![Linux Test Status](https://github.com/mdlayher/consrv/workflows/Linux%20Test/badge.svg)](https://github.com/mdlayher/consrv/actions) [![GoDoc](https://godoc.org/github.com/mdlayher/consrv?status.svg)](https://godoc.org/github.com/mdlayher/consrv) [![Go Report Card](https://goreportcard.com/badge/github.com/mdlayher/consrv)](https://goreportcard.com/report/github.com/mdlayher/consrv)

Command `consrv` is a SSH to serial console bridge server, originally designed
for deployment on [gokrazy.org](https://gokrazy.org) devices. Apache 2.0 Licensed.

## Overview

SSH can be used to conveniently access remote machines over the network, but
only if the machine has functional networking.

Serial consoles can be used to remotely access a machine with broken or no
networking, but often require running a cable from another machine to remotely
rescue a machine.

`consrv` combines the best of both worlds: an SSH interface running on a
Raspberry Pi which can provide serial console access to one or more remote
machines, all secured by an SSH channel. I (Matt Layher) run `consrv` on two
Raspberry Pi 4s using gokrazy to act as remote serial console servers for my
headless machines.

```text
-- Ethernet --> [Raspberry Pi + consrv]
                  |-- USB to serial --> [desktop]
                  |-- USB to serial --> [router]
                  |-- USB to serial --> [server]
```

I use the following hardware, but any serial equipment supported by Linux should
just work:

- [Syba dual port DB9 COM RS232 PCIe x1
  card](https://www.amazon.com/gp/product/B003D3MFHM/)
- [StarTech USB to Serial RS232 null-modem adapter](https://www.amazon.com/gp/product/B008634VJY/)

## Setup (gokrazy)

After formatting and mounting `/perm` on a gokrazy device, create the following
files:

- `/perm/consrv/host_key`: an OpenSSH format private key for the host (generate
  using `ssh-keygen`, I recommend `ssh-keygen -t ed25519`)
- `/perm/consrv/consrv.toml`: the configuration file for `consrv`

## Setup (Linux/other OS)

When `consrv` is built for a non-gokrazy Linux or other operating system
(without build tag `gokrazy`), flags are available to specify the location of
the configuration and SSH host key files:

```
$ ./consrv -h
Usage of ./consrv:
  -c string
        path to consrv.toml configuration file (default "consrv.toml")
  -k string
        path to OpenSSH format host key file (default "host_key")
```

## Configuration

The TOML configuration file should have device entries for each serial device,
and SSH public key identities which can be used to access the devices. Password
authentication is not supported. For example:

```toml
# Configure the SSH server listener. If no configuration is specified, consrv
# binds the SSH server to ":2222" by default.
[server]
address = ":2222"

# Configure one or more USB to serial devices with friendly names which are used
# as the SSH username to access a device's serial console. You must specify either
# "device" as the path to the device or "serial" to look up the device's path
# by the adapter's serial number (useful for machines with many connections).
#
# Optionally a list of identities which are allowed to access a device may be
# provided on a per-device basis. If no identities key is configured, all
# identities are allowed to access the device.
[[devices]]
name = "server"
serial = "A64NMAJS"
baud = 115200
identities = ["mdlayher"]

[[devices]]
name = "desktop"
device = "/dev/ttyUSB1"
baud = 115200

# Configure one or more SSH public key identities which can authenticate against
# consrv to access the devices.
[[identities]]
name = "mdlayher"
public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIN5i5d0mRKAf02m+ju+I1KrAYw3Ny2IHXy88mgyragBN Matt Layher (mdlayher@gmail.com)"

# Enable or disable the debug HTTP server for facilities such as Prometheus
# metrics and pprof support.
#
# Warning: do not expose pprof on an untrusted network!
[debug]
address = "localhost:9288"
prometheus = true
pprof = false
```

Now you can log in to either device's serial console over SSH using port 2222 on
the `consrv` host. When you're ready to end your session, use the SSH escape
`ENTER ~ .` to break the connection:

```text
$ ssh -i ~/.ssh/mdlayher_ed25519 -p 2222 server@monitnerr-1
consrv> opened serial connection "server": path: "/dev/ttyUSB0", serial: "A64NMAJS", baud: 115200

servnerr-3 login: matt
Password:

[matt@servnerr-3:~]$ w
 19:49:16 up 8 days,  1:01,  1 user,  load average: 0.12, 0.06, 0.02
USER     TTY        LOGIN@   IDLE   JCPU   PCPU WHAT
matt     ttyS0     19:49    4.00s  0.03s  0.00s w

[matt@servnerr-3:~]$ Shared connection to monitnerr-1 closed.
```
