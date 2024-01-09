//go:build !gokrazy

package main

import (
	"log"
	"sync"
)

func dropPrivileges(_ *sync.Cond, ll *log.Logger) {
	ll.Printf("Privdrop is not implemented for this platform yet")
}
