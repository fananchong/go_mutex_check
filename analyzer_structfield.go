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

func (analyzer *StructFieldAnalyzer) FindVar(pass *analysis.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch t := node.(type) {
			case *ast.TypeSpec:
				if structType, ok := t.Type.(*ast.StructType); ok {
					fields := structType.Fields.List
					for _, field := range fields {
						if isMutexType(field.Type) {
							m := analyzer.getStructFieldByPos(analyzer.prog, pass.Fset.Position(field.Pos()))

							comment := ""
							if field.Comment != nil {
								comment = strings.ReplaceAll(field.Comment.Text(), " ", "")
								comment = strings.ReplaceAll(comment, "\n", "")
							}
							if comment == "" {
								pos := pass.Fset.Position(field.Pos())
								fmt.Printf("[mutex check] %v:%v mutex 变量没有注释，指明它要锁的变量\n", pos.Filename, pos.Line)
								continue
							}
							if strings.Contains(comment, "nolint") {
								continue
							}
							mutexFiled := field
							varNames := strings.Split(comment, ",")
							for _, name := range varNames {
								varFiled := analyzer.getStructFieldByName(fields, name)
								if varFiled == nil {
									pos := pass.Fset.Position(mutexFiled.Pos())
									fmt.Printf("[mutex check] %v:%v mutex 变量注释中的变量 %v ，未声明\n", pos.Filename, pos.Line, name)
									break
								} else {
									v := analyzer.getStructFieldByPos(analyzer.prog, pass.Fset.Position(varFiled.Pos()))
									analyzer.vars[v] = m
								}
							}
						}
					}
				}
			}
			return true
		})
	}
}

func (analyzer *StructFieldAnalyzer) FindCaller(edge *callgraph.Edge, seen map[*callgraph.Node]bool) error {
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
	for _, block := range caller.Func.Blocks {
		for _, instr := range block.Instrs {
			if instr.Pos() == token.NoPos {
				continue
			}
			if fieldAddr, ok := instr.(*ssa.FieldAddr); ok && fieldAddr.X != nil {
				if pointerType, ok := fieldAddr.X.Type().Underlying().(*types.Pointer); ok {
					if structType, ok := pointerType.Elem().Underlying().(*types.Struct); ok {
						field := structType.Field(fieldAddr.Field)
						for k := range analyzer.vars {
							if k == field {
								if _, ok := analyzer.callers[k]; !ok {
									analyzer.callers[k] = make(map[*callgraph.Node][]token.Position)
								}
								analyzer.callers[k][caller] = []token.Position{}
								break
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func (analyzer *StructFieldAnalyzer) CheckVarLock(prog *ssa.Program, caller *callgraph.Node, mymutex, myvar *types.Var) (poss []token.Position) {
	var mInstrs []ssa.Instruction
	var vInstrs []ssa.Instruction
	for _, block := range caller.Func.Blocks {
		mInstrs = append(mInstrs, analyzer.findInstrByStructFieldCall(block, mymutex)...)
		vInstrs = append(vInstrs, analyzer.findInstrByStructField(block, myvar)...)
	}
	for _, vInstr := range vInstrs {
		vPos := prog.Fset.Position(vInstr.Pos())
		if !checkMutexLock(prog, mInstrs, vPos) {
			poss = append(poss, caller.Func.Prog.Fset.Position(vInstr.Pos()))
		}
	}
	return
}

func (analyzer *StructFieldAnalyzer) HaveVar(prog *ssa.Program, caller *callgraph.Node, m *types.Var) bool {
	var find bool
	for _, block := range caller.Func.Blocks {
		mInstr := analyzer.findInstrByStructField(block, m)
		if len(mInstr) > 0 {
			find = true
			break
		}
	}
	return find
}

func (analyzer *StructFieldAnalyzer) CheckCallLock(prog *ssa.Program, caller *callgraph.Node, mymutex *types.Var, callee *callgraph.Node) bool {
	var mInstrs []ssa.Instruction
	for _, block := range caller.Func.Blocks {
		mInstrs = append(mInstrs, analyzer.findInstrByStructFieldCall(block, mymutex)...)
	}
	for _, vPos := range getCalleePostion(prog, caller, callee) {
		if !checkMutexLock(prog, mInstrs, vPos) {
			return false
		}
	}
	return true
}

func (analyzer *StructFieldAnalyzer) getStructFieldByPos(prog *ssa.Program, pos token.Position) *types.Var {
	for _, pkg := range prog.AllPackages() {
		for _, member := range pkg.Members {
			if obj, ok := member.(*ssa.Type); ok {
				if s, ok := obj.Type().Underlying().(*types.Struct); ok {
					for i := 0; i < s.NumFields(); i++ {
						field := s.Field(i)
						p := prog.Fset.Position(field.Pos())
						if p == pos {
							return field
						}
					}
				}
			}
		}
	}
	return nil
}

func (analyzer *StructFieldAnalyzer) findInstrByStructField(block *ssa.BasicBlock, v *types.Var) (instrs []ssa.Instruction) {
	for _, instr := range block.Instrs {
		if fieldAddr, ok := instr.(*ssa.FieldAddr); ok && fieldAddr.X != nil {
			if pointerType, ok := fieldAddr.X.Type().Underlying().(*types.Pointer); ok {
				if structType, ok := pointerType.Elem().Underlying().(*types.Struct); ok {
					field := structType.Field(fieldAddr.Field)
					if field == v {
						instrs = append(instrs, instr)
					}
				}
			}
		}
	}
	return
}

func (analyzer *StructFieldAnalyzer) findInstrByStructFieldCall(block *ssa.BasicBlock, v *types.Var) (instrs []ssa.Instruction) {
	for _, instr := range block.Instrs {
		if c, ok := instr.(*ssa.Call); ok {
			if c.Call.Signature().Recv() != nil && c.Call.Args != nil {
				arg := c.Call.Args[0]
				if fieldAddr, ok := arg.(*ssa.FieldAddr); ok && fieldAddr.X != nil {
					if pointerType, ok := fieldAddr.X.Type().Underlying().(*types.Pointer); ok {
						if structType, ok := pointerType.Elem().Underlying().(*types.Struct); ok {
							field := structType.Field(fieldAddr.Field)
							if field == v {
								instrs = append(instrs, instr)
							}
						}
					}
				}
			}
		}
	}
	return
}

func (analyzer *StructFieldAnalyzer) getStructFieldByName(fields []*ast.Field, name string) *ast.Field {
	for _, field := range fields {
		var n string
		if len(field.Names) > 0 {
			n = field.Names[0].Name
		} else if v, ok := field.Type.(*ast.SelectorExpr); ok {
			n = v.Sel.Name
		} else if v, ok := field.Type.(*ast.StarExpr); ok {
			if v2, ok := v.X.(*ast.Ident); ok {
				n = v2.Name
			} else if v2, ok := v.X.(*ast.SelectorExpr); ok {
				n = v2.Sel.Name
			} else {
				panic("getStructFieldByName, here #1")
			}
		} else if v, ok := field.Type.(*ast.Ident); ok {
			n = v.Name
		} else {
			panic("getStructFieldByName, here #2")
		}
		if n == name {
			return field
		}
	}
	return nil
}

type StructFieldAnalyzer struct {
	*BaseAnalyzer
}

func NewStructFieldAnalyzer(path string, cg *callgraph.Graph, prog *ssa.Program) *StructFieldAnalyzer {
	analyzer := &StructFieldAnalyzer{}
	analyzer.BaseAnalyzer = NewBaseAnalyzer(path, cg, prog)
	analyzer.Derive = analyzer
	return analyzer
}
