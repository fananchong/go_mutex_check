package main

import (
	"flag"
	"fmt"
	"sort"
)

var path string

func main() {
	flag.StringVar(&path, "path", ".", "package path")
	flag.Parse()

	cg, prog, err := doCallgraph("vta", false, []string{fmt.Sprintf("%s/...", path)})
	if err != nil {
		panic(err)
	}

	analyzer1 := NewVarAnalyzer(path, cg, prog)
	analyzer1.Analysis()

	analyzer2 := NewStructFieldAnalyzer(path, cg, prog)
	analyzer2.Analysis()

	s := append(analyzer1.Prints, analyzer2.Prints...)
	sort.Sort(s)
	for _, v := range s {
		fmt.Println(v)
	}
}
