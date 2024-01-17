# CHANGELOG

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
