package test

import (
	"fmt"
	"sync"
)

var m1 sync.Mutex // a1
var a1 int

func f1() {
	m1.Lock()
	// defer m1.Unlock()
	fmt.Print(a1)
}
