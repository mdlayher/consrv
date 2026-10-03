# CHANGELOG

# Unreleased

- Fixed `logtostdout` stalling a device: a line longer than 64 KiB, common
  on firmware setup screens which draw with cursor movement, stopped the
  logging goroutine, which then blocked every SSH session on that device.
  Logging now never blocks a device's sessions, dropping output (and saying
  so) when standard output falls behind.
- `logtostdout` lines now end at any of `\n`, `\r`, or `\r\n`, are split
  past 4096 bytes, and are logged after a second of idle output so a prompt
  with no line ending still appears. Control characters, backslashes, and
  bytes which are not valid UTF-8 are escaped as `\xNN` or `\\`.
- `logtostdout` lines are always prefixed with the device name, even when
  only one device logs, and consrv's own log messages are prefixed with
  `consrv> `, matching what it prints to SSH sessions.
- Fixed device output being truncated for a client reading with a buffer
  smaller than one read from the device.

# v1.3.0
August 31, 2026

- Support for matching one port of a multi-port USB to serial adapter (such as
  a quad FTDI cable) whose ports all share a single serial number, using the
  new optional `interface` device configuration key alongside `serial`.
- Device enumeration now logs each device's USB interface number and warns
  when multiple devices share a serial number. Configuring a shared serial
  number without an `interface` key is now an error rather than silently
  matching whichever port was enumerated last.
- Updated to Go 1.27 and the latest versions of all dependencies.

# v1.2.1
December 12, 2024

- Bump dependencies for [CVE-2024-45337](https://github.com/advisories/GHSA-v778-237x-gjrc).
  - See also https://github.com/golang/go/issues/70779.

# v1.2.0
January 17, 2024

- Added an *experimental* (may have breaking changes in v1.x if necessary)
  `-experimental-drop-privileges` flag which is only available when running on
  gokrazy. After reading configuration and opening network listeners, consrv
  will:
  - chroot the process into an empty directory
  - set user and group to nobody/nobody

Thank you [@bdd](https://github.com/bdd) for the contribution.

## v1.1.0
July 20, 2023

- Ability to log device serial console to stdout.
- Support for enumerating `/dev/ttyACM*` devices.

## v1.0.0
April 15, 2022

First stable release!
