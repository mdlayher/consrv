//go:build gokrazy

package main

import (
	"log"
	"os"
	"sync"
	"syscall"
)

const (
	PRIVDROP_UID      = 65534 // conventionally: nobody
	PRIVDROP_GID      = 65534 // conventionally: nogroup
	CHROOT_PARENT_DIR = "/dev/shm"
	CHROOT_DIR_PATTERN = "consrv-chroot-*"
)

func dropPrivileges(cond *sync.Cond, ll *log.Logger) {
	ll.Printf("droping privileges")
	chrootDir, err := os.MkdirTemp(CHROOT_PARENT_DIR, CHROOT_DIR_PATTERN)
	if err != nil {
		ll.Fatalf("couldn't create an empty directory under %s to chroot into: %v", CHROOT_PARENT_DIR, err)
	}

	err = syscall.Chroot(chrootDir)
	if err != nil {
		ll.Fatalf("couldn't chroot to %s: %v", chrootDir, err)
	}
	ll.Printf("chroot'ed into %s", chrootDir)

	err = syscall.Setgid(PRIVDROP_GID)
	if err != nil {
		ll.Fatalf("couldn't setgid to %d: %v", PRIVDROP_GID, err)
	}

	err = syscall.Setuid(PRIVDROP_UID)
	if err != nil {
		ll.Fatalf("couldn't setuid to %d: %v", PRIVDROP_UID, err)
	}

	ll.Printf("changed to uid=%d gid=%d", PRIVDROP_UID, PRIVDROP_GID)

	cond.Broadcast()
}
