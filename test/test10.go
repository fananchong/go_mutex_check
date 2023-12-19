package test

import "sync"

var (
	m1 = sync.RWMutex{}
	m2 = &sync.RWMutex{}
)
