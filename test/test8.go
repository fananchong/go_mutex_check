package test

import (
	"fmt"
	"sync"
)

type b struct {
	*sync.Mutex // a
	a           int
}

func (b1 *b) f1() {
	b1.Lock()
	// defer b1.Unlock()
	fmt.Print(b1.a)
}

func init() {
	b1 := b{}
	b1.f1()
}
