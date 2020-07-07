//+build !gokrazy

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
