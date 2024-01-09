package main

import (
	"sync"
)

func newPrivdropCond() *sync.Cond {
	return sync.NewCond(new(sync.Mutex))
}

func waitForCond(cond *sync.Cond) {
	cond.L.Lock()
	cond.Wait()
	cond.L.Unlock()
}
