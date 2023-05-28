package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
)

func (analyzer *VarAnalyzer) FindVar(pass *analysis.Pass) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.VAR {
				continue
			}
			var mutexValueSpec *ast.ValueSpec
			for _, spec := range genDecl.Specs {
				if valueSpec, ok := spec.(*ast.ValueSpec); ok {
					if !isMutexType(valueSpec.Type) {
						continue
					}
					ident := valueSpec.Names[0]
					obj := pass.TypesInfo.Defs[ident]
					if obj == nil {
						continue
					}
					v, _ := obj.(*types.Var)
					isGlobal := !v.IsField() && !v.Embedded() && v.Parent() == pass.Pkg.Scope() // 全局变量
					if isGlobal {
						mutexValueSpec = valueSpec
					}
				}
			}
			if mutexValueSpec == nil {
				continue
			}
			pos := pass.Fset.Position(mutexValueSpec.Pos())
			mutexVar := analyzer.getGlobalVarByPos(analyzer.prog, pos)
			comment := ""
			if mutexValueSpec.Comment != nil {
				comment = strings.ReplaceAll(mutexValueSpec.Comment.Text(), " ", "")
				comment = strings.ReplaceAll(comment, "\n", "")
			}
			if comment == "" {
				fmt.Printf("[mutex check] %v:%v mutex 变量没有注释，指明它要锁的变量\n", pos.Filename, pos.Line)
				continue
			}
			if strings.Contains(comment, "nolint") {
				continue
			}
			varNames := strings.Split(comment, ",")
			for _, name := range varNames {
				valueSpec := analyzer.getGlobalVarByName(pass, file, name)
				if valueSpec == nil {
					pos := pass.Fset.Position(mutexValueSpec.Pos())
					fmt.Printf("[mutex check] %v:%v mutex 变量注释中的变量 %v ，未声明\n", pos.Filename, pos.Line, name)
					break
				} else {
					pos := pass.Fset.Position(valueSpec.Pos())
					v := analyzer.getGlobalVarByPos(analyzer.prog, pos)
					analyzer.vars[v] = mutexVar
				}
			}
		}
	}
}

func (analyzer *VarAnalyzer) FindCaller(edge *callgraph.Edge, seen map[*callgraph.Node]bool) error {
	caller := edge.Caller
	if seen[caller] {
		return nil
	}
	if caller.Func == nil {
		return nil
	}
	seen[caller] = true
	if caller.Func.Name() == "init" {
		return nil
	}
	usesVar := func(instr ssa.Instruction, v *types.Var) bool {
		for _, op := range instr.Operands(nil) {
			if varRef, ok := (*op).(*ssa.Global); ok && varRef.Object() == v {
				return true
			}
		}
		return false
	}
	for _, block := range caller.Func.Blocks {
		for _, instr := range block.Instrs {
			for k := range analyzer.vars {
				if usesVar(instr, k) {
					if _, ok := analyzer.callers[k]; !ok {
						analyzer.callers[k] = make(map[*callgraph.Node][]token.Position)
					}
					analyzer.callers[k][caller] = []token.Position{}
				}
			}
		}
	}
	return nil
}

func (analyzer *VarAnalyzer) CheckVarLock(prog *ssa.Program, caller *callgraph.Node, mymutex, myvar *types.Var) (poss []token.Position) {
	var mInstrs []ssa.Instruction
	var vInstrs []ssa.Instruction
	for _, block := range caller.Func.Blocks {
		mInstrs = append(mInstrs, analyzer.findInstrByGlobalVar(block, mymutex)...)
		vInstrs = append(vInstrs, analyzer.findInstrByGlobalVar(block, myvar)...)
	}
	for _, vInstr := range vInstrs {
		if !checkVar(prog, mInstrs, vInstr) {
			poss = append(poss, caller.Func.Prog.Fset.Position(vInstr.Pos()))
		}
	}
	return
}

func (analyzer *VarAnalyzer) HaveVar(prog *ssa.Program, caller *callgraph.Node, m *types.Var) bool {
	var find bool
	for _, block := range caller.Func.Blocks {
		mInstr := analyzer.findInstrByGlobalVar(block, m)
		if len(mInstr) > 0 {
			find = true
			break
		}
	}
	return find
}

func (analyzer *VarAnalyzer) getGlobalVarByPos(prog *ssa.Program, pos token.Position) *types.Var {
	for _, pkg := range prog.AllPackages() {
		for _, member := range pkg.Members {
			if global, ok := member.(*ssa.Global); ok {
				p := prog.Fset.Position(global.Pos())
				if p == pos {
					return global.Object().(*types.Var)
				}
			}
		}
	}
	return nil
}

func (analyzer *VarAnalyzer) findInstrByGlobalVar(block *ssa.BasicBlock, v *types.Var) (instrs []ssa.Instruction) {
	for _, instr := range block.Instrs {
		for _, op := range instr.Operands(nil) {
			if varRef, ok := (*op).(*ssa.Global); ok && varRef.Object() == v {
				instrs = append(instrs, instr)
			}
		}
	}
	return
}

func (analyzer *VarAnalyzer) getGlobalVarByName(pass *analysis.Pass, file *ast.File, name string) *ast.ValueSpec {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.VAR {
			continue
		}
		for _, spec := range genDecl.Specs {
			if valueSpec, ok := spec.(*ast.ValueSpec); ok {
				ident := valueSpec.Names[0]
				obj := pass.TypesInfo.Defs[ident]
				if obj == nil {
					continue
				}
				v, _ := obj.(*types.Var)
				isGlobal := !v.IsField() && !v.Embedded() && v.Parent() == pass.Pkg.Scope() // 全局变量
				if isGlobal && ident.Name == name {
					return valueSpec
				}
			}
		}
	}
	return nil
}

type VarAnalyzer struct {
	*BaseAnalyzer
}

func NewVarAnalyzer(path string, cg *callgraph.Graph, prog *ssa.Program) *VarAnalyzer {
	analyzer := &VarAnalyzer{}
	analyzer.BaseAnalyzer = NewBaseAnalyzer(path, cg, prog)
	analyzer.Derive = analyzer
	return analyzer
}
