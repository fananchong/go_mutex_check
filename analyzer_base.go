package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
)

type IAnalysis interface {
	FindVar(pass *analysis.Pass)
	FindCaller(*callgraph.Edge, map[*callgraph.Node]bool) error
	CheckVarLock(prog *ssa.Program, caller *callgraph.Node, mymutex, myvar *types.Var) []token.Position
	HaveVar(prog *ssa.Program, caller *callgraph.Node, m *types.Var) bool
}

func (analyzer *BaseAnalyzer) runOne(prog *ssa.Program, pass *analysis.Pass) (interface{}, error) {
	analyzer.Derive.FindVar(pass)
	return nil, nil
}

func (analyzer *BaseAnalyzer) step2FindCaller() {
	seen := make(map[*callgraph.Node]bool)
	if err := callgraph.GraphVisitEdges(analyzer.cg, func(edge *callgraph.Edge) error { return analyzer.Derive.FindCaller(edge, seen) }); err != nil {
		panic(err)
	}
}

func (analyzer *BaseAnalyzer) step3CutCaller() {
	for v := range analyzer.callers {
		m := analyzer.vars[v]
		callers := analyzer.callers[v]
		for caller := range callers {
			poss := analyzer.Derive.CheckVarLock(analyzer.prog, caller, m, v)
			if len(poss) != 0 {
				if _, ok := analyzer.callers2[v]; !ok {
					analyzer.callers2[v] = make(map[*callgraph.Node][]token.Position)
				}
				analyzer.callers2[v][caller] = poss
			}
		}
	}
}

func (analyzer *BaseAnalyzer) step4CheckPath(myvar *types.Var, target *callgraph.Node, paths []*callgraph.Node, seen map[*callgraph.Node]bool, checkFail *string) {
	if seen[target] {
		return
	}
	seen[target] = true

	if *checkFail != "" {
		return
	}

	newPaths := append([]*callgraph.Node{target}, paths...)
	var looped bool
	for _, v := range paths {
		if v.Func == target.Func {
			looped = true
			break
		}
	}

	// 检查是否有 mutex (上层函数)
	mymutex := analyzer.vars[myvar]
	if len(newPaths) > 1 && analyzer.Derive.HaveVar(analyzer.prog, target, mymutex) {
		return
	}

	// 如果超出本包，则报错
	if target.Func.Pkg.Pkg != myvar.Pkg() {
		*checkFail = printPaht(newPaths, looped)
		return
	}

	// 如果已经是协程起点，则报错
	if isGoroutine(target.Func) {
		*checkFail = printPaht(newPaths, looped)
		return
	}

	if len(target.In) == 0 || looped {
		*checkFail = printPaht(newPaths, looped)
		return
	} else {
		for _, in := range target.In {
			*checkFail = ""
			analyzer.step4CheckPath(myvar, in.Caller, newPaths, seen, checkFail)
		}
	}
}

type BaseAnalyzer struct {
	*analysis.Analyzer
	path     string
	cg       *callgraph.Graph
	prog     *ssa.Program
	vars     map[*types.Var]*types.Var // key : 变量； value mutex
	callers  map[*types.Var]map[*callgraph.Node][]token.Position
	callers2 map[*types.Var]map[*callgraph.Node][]token.Position
	Prints   sort.StringSlice
	Derive   IAnalysis
}

func NewBaseAnalyzer(path string, cg *callgraph.Graph, prog *ssa.Program) *BaseAnalyzer {
	analyzer := &BaseAnalyzer{
		path:     path,
		cg:       cg,
		prog:     prog,
		vars:     map[*types.Var]*types.Var{},
		callers:  map[*types.Var]map[*callgraph.Node][]token.Position{},
		callers2: map[*types.Var]map[*callgraph.Node][]token.Position{},
	}
	analyzer.Analyzer = &analysis.Analyzer{
		Name: "mutex_check",
		Doc:  "mutex check",
		Run:  func(p *analysis.Pass) (interface{}, error) { return analyzer.runOne(prog, p) },
	}
	return analyzer
}

func (analyzer *BaseAnalyzer) Analysis() {
	err := Analysis(analyzer.path, analyzer.Analyzer)
	if err != nil {
		panic(err)
	}
	// 2. 获取哪些函数 B ，直接使用了相关字段
	analyzer.step2FindCaller()
	// 3. 剔除 B 中有加锁的函数，得 C
	analyzer.step3CutCaller()
	// 4. 查看调用关系，逆向检查上级调用是否加锁
	seen := make(map[string]bool)

	var keys sort.StringSlice
	m := map[string]*types.Var{}
	for v := range analyzer.callers2 {
		n := fmt.Sprintf("%v_%v", v.Pkg().Path(), v.Name())
		keys = append(keys, n)
		m[n] = v
	}
	sort.Sort(keys)
	for _, key := range keys {
		v := m[key]
		nodes := analyzer.callers2[v]
		for node, varCallPos := range nodes {
			var checkFail string
			analyzer.step4CheckPath(v, node, []*callgraph.Node{}, map[*callgraph.Node]bool{}, &checkFail)
			if checkFail != "" {
				if _, ok := seen[checkFail]; !ok {
					for _, pos := range varCallPos {
						var s string
						if pos.Filename != "" && pos.Line != 0 {
							s = fmt.Sprintf("[mutex lint] %v:%v 没有调用 mutex lock 。", pos.Filename, pos.Line)
						}
						if s != "" {
							analyzer.Prints = append(analyzer.Prints, s)
						}
					}
				}
				seen[checkFail] = true
			}
			checkFail = ""
		}
	}
}

func isSyncMutexType(expr ast.Expr) bool {
	ident, ok := expr.(*ast.SelectorExpr)
	if !ok || ident.X == nil || ident.Sel == nil {
		return false
	}
	x, ok := ident.X.(*ast.Ident)
	sel := ident.Sel
	if !ok {
		return false
	}
	return sel.Name == "Mutex" && x.Name == "sync"
}

func isSyncRWMutexType(expr ast.Expr) bool {
	ident, ok := expr.(*ast.SelectorExpr)
	if !ok || ident.X == nil || ident.Sel == nil {
		return false
	}
	x, ok := ident.X.(*ast.Ident)
	sel := ident.Sel
	if !ok {
		return false
	}
	return sel.Name == "RWMutex" && x.Name == "sync"
}

func isMutexType(expr ast.Expr) bool {
	return isSyncMutexType(expr) || isSyncRWMutexType(expr)
}

func checkVar(prog *ssa.Program, mInstrs []ssa.Instruction, vInstr ssa.Instruction) bool {
	if mInstrs == nil {
		return false
	}
	if mInstrs != nil {
		// 如果有 defer unlock ，只要看有 Lock 在前面
		var hasDefer bool
		for _, instr := range mInstrs {
			if d, ok := instr.(*ssa.Defer); ok && (d.Call.Value.Name() == "Unlock" || d.Call.Value.Name() == "RUnlock") {
				hasDefer = true
				break
			}
		}
		vPos := prog.Fset.Position(vInstr.Pos())
		if hasDefer {
			for i := 0; i < len(mInstrs); i++ {
				switch c := mInstrs[i].(type) {
				case *ssa.Defer:
					continue
				case *ssa.Call:
					n := c.Call.Value.Name()
					if n == "Unlock" || n == "RUnlock" {
						continue
					}
					mPos1 := prog.Fset.Position(mInstrs[i].Pos())
					if vPos.Line > mPos1.Line {
						return true
					}
				}
			}
		} else {
			// 否则，查看是否变量在  lock unlock 中间
			m := make(map[token.Position]token.Position)
			for i := 0; i < len(mInstrs); i++ {
				if c, ok := mInstrs[i].(*ssa.Call); ok {
					n := c.Call.Value.Name()
					if n == "Unlock" || n == "RUnlock" {
						continue
					}
					// 找 lock
					mPos1 := prog.Fset.Position(mInstrs[i].Pos())
					// 找 unlock
					var mPos2 token.Position
					for j := i + 1; j < len(mInstrs); j++ {
						if c, ok := mInstrs[i].(*ssa.Call); ok {
							n := c.Call.Value.Name()
							if n == "Lock" || n == "RLock" {
								break
							}
							mPos2 = prog.Fset.Position(mInstrs[j].Pos())
						}
					}
					m[mPos1] = mPos2
				}
			}
			for pos1, pos2 := range m {
				if pos2.Line != 0 {
					if vPos.Line > pos1.Line && vPos.Line < pos2.Line {
						return true
					}
				} else {
					if vPos.Line > pos1.Line {
						return true
					}
				}
			}
		}
	}
	return false
}

func printPaht(newPath []*callgraph.Node, looped bool) string {
	s := newPath[0].Func.String()
	for i := 1; i < len(newPath); i++ {
		s += " --> " + newPath[i].Func.String()
	}
	if looped {
		s += " [LOOP]"
	}
	return s
}

func isGoroutine(fn *ssa.Function) bool {
	if fn.Referrers() != nil {
		for _, r := range *fn.Referrers() {
			if _, ok := r.(*ssa.Go); ok {
				return true
			}
		}
	}
	return false
}
