package main

import (
	"flag"
	"fmt"
	"sort"
)

var path string
var buildFlag string

func main() {
	flag.StringVar(&path, "path", ".", "package path")
	flag.StringVar(&buildFlag, "buildflag", "--tags=", "build flag")
	flag.Parse()

	cg, prog, err := doCallgraph("vta", false, getAllPackageName(path))
	if err != nil {
		panic(err)
	}

	analyzer1 := NewVarAnalyzer(path, cg, prog)
	analyzer1.Analysis()

	analyzer2 := NewStructFieldAnalyzer(path, cg, prog)
	analyzer2.Analysis()

	m := map[string]bool{}
	s := append(analyzer1.PrintsCall, analyzer2.PrintsCall...)
	sort.Sort(s)
	for _, v := range s {
		if _, ok := m[v]; ok {
			continue
		}
		m[v] = true
		fmt.Println(v)
	}

	m = map[string]bool{}
	s = append(analyzer1.PrintsReturn, analyzer2.PrintsReturn...)
	sort.Sort(s)
	for _, v := range s {
		if _, ok := m[v]; ok {
			continue
		}
		m[v] = true
		fmt.Println(v)
	}
}
