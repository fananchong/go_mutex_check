package main

import (
	"go/token"

	"golang.org/x/tools/go/packages"
)

var comments = map[token.Position]string{}

func analysisComment(pkg *packages.Package) error {
	for _, file := range pkg.Syntax {
		for _, comment := range file.Comments {
			for _, l := range comment.List {
				pos := pkg.Fset.Position(l.Pos())
				pos.Column = 0
				pos.Offset = 0
				comments[pos] = l.Text
			}
		}
	}
	return nil
}

func getComment(pos token.Position) string {
	pos.Column = 0
	pos.Offset = 0
	if v, ok := comments[pos]; ok {
		return v
	}
	return ""
}
