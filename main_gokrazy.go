//+build gokrazy

package main

// filePaths provides hardcoded /perm paths on gokrazy.
func filePaths() (cfg string, hostKey string) {
	return "/perm/consrv/consrv.toml", "/perm/consrv/host_key"
}
