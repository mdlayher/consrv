# CHANGELOG

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
