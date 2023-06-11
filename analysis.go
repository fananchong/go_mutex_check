package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

func Analysis(path string, analyzer *analysis.Analyzer) error {
	packages, err := packages.Load(&packages.Config{
		Mode: packages.LoadAllSyntax, // nolint:staticcheck
	}, getAllPackageName(path)...)
	if err != nil {
		return err
	}
	// comment
	for _, pkg := range packages {
		_ = analysisComment(pkg)
	}
	pass := &analysis.Pass{
		Analyzer: analyzer,
		Files:    []*ast.File{},
		ResultOf: map[*analysis.Analyzer]interface{}{},
	}
	for _, pkg := range packages {
		if len(pkg.Errors) > 0 {
			return pkg.Errors[0]
		}
		pass.Fset = pkg.Fset
		pass.Files = pkg.Syntax
		pass.TypesInfo = pkg.TypesInfo
		pass.Pkg = pkg.Types
		_, err := analyzer.Run(pass)
		if err != nil {
			return err
		}
	}
	return nil
}

func getAllPackageName(root string) (packages []string) {
	m := make(map[string]int)
	var rootmodule string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if pkgName, err := getPackageName(path); err == nil {
				pkgName = strings.TrimSpace(pkgName)
				if path == root {
					rootmodule = pkgName
				}
				if rootmodule != "" {
					if !strings.HasPrefix(pkgName, rootmodule) {
						return nil
					}
				}
				v := strings.TrimSpace(pkgName) + "/..."
				m[v] = 1
			}
		}
		return nil
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	for k := range m {
		packages = append(packages, k)
	}
	return
}

func getPackageName(dir string) (string, error) {
	goModPath := filepath.Join(dir, "go.mod")
	file, err := os.Open(goModPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	buf := bufio.NewReader(file)
	line, _, _ := buf.ReadLine()
	packageName := line[6:] // 去除 "module " 前缀
	return string(packageName), nil
}
