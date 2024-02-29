package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/types"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/analysis/singlechecker"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
	"log"
)

//nolint:gochecknoglobals
var flagSet flag.FlagSet

func NewAnalyzer() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     "krt",
		Doc:      "calculates cyclomatic complexity",
		Run:      run,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Flags:    flagSet,
	}
}

type visit struct {
	t    *types.Info
	pass *analysis.Pass
}

func (v visit) Visit(n ast.Node) (w ast.Visitor) {
	switch n := n.(type) {
	case *ast.CallExpr:
		check := func(id *ast.Ident) bool {
			obj := v.t.ObjectOf(id)
			return (obj.Name() == "Fetch" || obj.Name() == "FetchOne") && obj.Pkg().Path() == "istio.io/istio/pkg/kube/krt"
		}
		switch f := n.Fun.(type) {
		case *ast.SelectorExpr:
			if check(f.Sel) {
				v.pass.Report(analysis.Diagnostic{Pos: n.Pos(), End: n.End(), Message: fmt.Sprintf("fetch %T %+v, %T", f.X, f.Sel, n.Args[1])})
			}
		}
	}
	return v
}

func run(pass *analysis.Pass) (interface{}, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.File)(nil),
		(*ast.RangeStmt)(nil),
		(*ast.ForStmt)(nil),
		(*ast.CallExpr)(nil),
	}
	inspect.Nodes(nodeFilter, func(n ast.Node, push bool) bool {
		if !push {
			// inspect.Nodes is slightly suboptimal as we only use push=true.
			return true
		}
		// Find the variables updated by the loop statement.
		var vars []types.Object
		addVar := func(expr ast.Expr) {
			if id, _ := expr.(*ast.Ident); id != nil {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					vars = append(vars, obj)
				}
			}
		}

		info := pass.TypesInfo
		var body *ast.BlockStmt
		switch n := n.(type) {
		case *ast.File:
			log.Println("analyze file", n.Name)
		case *ast.CallExpr:
			//pass.Report(analysis.Diagnostic{Pos: n.Pos(), End: n.End(), Message:
			//fmt.Sprintf("call %T", n.Fun),
			//})
			check := func(id *ast.Ident) bool {
				obj := info.ObjectOf(id)
				return obj.Name() == "NewCollection" && obj.Pkg().Path() == "istio.io/istio/pkg/kube/krt"
			}
			switch f := n.Fun.(type) {
			case *ast.SelectorExpr:
				if check(f.Sel) {
					pass.Report(analysis.Diagnostic{Pos: n.Pos(), End: n.End(), Message: fmt.Sprintf("call %T %+v, %T", f.X, f.Sel, n.Args[1])})
					switch xfm := n.Args[1].(type) {
					case *ast.FuncLit:
						ast.Walk(visit{
							t:    info,
							pass: pass,
						}, xfm)
						//forEachStmt(xfm.Body.List, func(last ast.Stmt) {
						//	pass.Report(analysis.Diagnostic{Pos: last.Pos(), Message: fmt.Sprintf("var %T ", last)})
						//})
					}
				}
			case *ast.Ident:
				//if fun, ok := findDecl(fun).(*ast.SelectorExpr); ok {
				//	return check(fun.Sel)
				//}
				//return false
			}
		case *ast.ForStmt:
			body = n.Body
			switch post := n.Post.(type) {
			case *ast.AssignStmt:
				// e.g. for p = head; p != nil; p = p.next
				for _, lhs := range post.Lhs {
					addVar(lhs)
				}
			case *ast.IncDecStmt:
				// e.g. for i := 0; i < n; i++
				addVar(post.X)
			}
		}
		if vars == nil {
			return true
		}

		// Inspect statements to find function literals that may be run outside of
		// the current loop iteration.
		//
		// For go, defer, and errgroup.Group.Go, we ignore all but the last
		// statement, because it's hard to prove go isn't followed by wait, or
		// defer by return. "Last" is defined recursively.
		//
		// TODO: consider allowing the "last" go/defer/Go statement to be followed by
		// N "trivial" statements, possibly under a recursive definition of "trivial"
		// so that that checker could, for example, conclude that a go statement is
		// followed by an if statement made of only trivial statements and trivial expressions,
		// and hence the go statement could still be checked.
		forEachLastStmt(body.List, func(last ast.Stmt) {
			var stmts []ast.Stmt
			switch s := last.(type) {
			case *ast.GoStmt:
				stmts = litStmts(s.Call.Fun)
			case *ast.DeferStmt:
				stmts = litStmts(s.Call.Fun)
			case *ast.ExprStmt: // check for errgroup.Group.Go
				if call, ok := s.X.(*ast.CallExpr); ok {
					stmts = litStmts(goInvoke(pass.TypesInfo, call))
				}
			}
			for _, stmt := range stmts {
				reportCaptured(pass, vars, stmt)
			}
		})

		// Also check for testing.T.Run (with T.Parallel).
		// We consider every t.Run statement in the loop body, because there is
		// no commonly used mechanism for synchronizing parallel subtests.
		// It is of course theoretically possible to synchronize parallel subtests,
		// though such a pattern is likely to be exceedingly rare as it would be
		// fighting against the test runner.
		for _, s := range body.List {
			switch s := s.(type) {
			case *ast.ExprStmt:
				if call, ok := s.X.(*ast.CallExpr); ok {
					for _, stmt := range parallelSubtest(pass.TypesInfo, call) {
						reportCaptured(pass, vars, stmt)
					}

				}
			}
		}
		return true
	})
	return nil, nil
}

// reportCaptured reports a diagnostic stating a loop variable
// has been captured by a func literal if checkStmt has escaping
// references to vars. vars is expected to be variables updated by a loop statement,
// and checkStmt is expected to be a statements from the body of a func literal in the loop.
func reportCaptured(pass *analysis.Pass, vars []types.Object, checkStmt ast.Stmt) {
	ast.Inspect(checkStmt, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		obj := pass.TypesInfo.Uses[id]
		if obj == nil {
			return true
		}
		for _, v := range vars {
			if v == obj {
				pass.ReportRangef(id, "loop variable %s captured by func literal", id.Name)
			}
		}
		return true
	})
}

// forEachLastStmt calls onLast on each "last" statement in a list of statements.
// "Last" is defined recursively so, for example, if the last statement is
// a switch statement, then each switch case is also visited to examine
// its last statements.
func forEachLastStmt(stmts []ast.Stmt, onLast func(last ast.Stmt)) {
	if len(stmts) == 0 {
		return
	}

	s := stmts[len(stmts)-1]
	switch s := s.(type) {
	case *ast.IfStmt:
	loop:
		for {
			forEachLastStmt(s.Body.List, onLast)
			switch e := s.Else.(type) {
			case *ast.BlockStmt:
				forEachLastStmt(e.List, onLast)
				break loop
			case *ast.IfStmt:
				s = e
			case nil:
				break loop
			}
		}
	case *ast.ForStmt:
		forEachLastStmt(s.Body.List, onLast)
	case *ast.RangeStmt:
		forEachLastStmt(s.Body.List, onLast)
	case *ast.SwitchStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			forEachLastStmt(cc.Body, onLast)
		}
	case *ast.TypeSwitchStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			forEachLastStmt(cc.Body, onLast)
		}
	case *ast.SelectStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CommClause)
			forEachLastStmt(cc.Body, onLast)
		}
	default:
		onLast(s)
	}
}
func forEachStmt(stmts []ast.Stmt, onLast func(last ast.Stmt)) {
	if len(stmts) == 0 {
		return
	}

	for _, s := range stmts {
		switch s := s.(type) {
		case *ast.IfStmt:
		loop:
			for {
				forEachStmt(s.Body.List, onLast)
				switch e := s.Else.(type) {
				case *ast.BlockStmt:
					forEachStmt(e.List, onLast)
					break loop
				case *ast.IfStmt:
					s = e
				case nil:
					break loop
				}
			}
		case *ast.ForStmt:
			forEachStmt(s.Body.List, onLast)
		case *ast.RangeStmt:
			forEachStmt(s.Body.List, onLast)
		case *ast.SwitchStmt:
			for _, c := range s.Body.List {
				cc := c.(*ast.CaseClause)
				forEachStmt(cc.Body, onLast)
			}
		case *ast.TypeSwitchStmt:
			for _, c := range s.Body.List {
				cc := c.(*ast.CaseClause)
				forEachStmt(cc.Body, onLast)
			}
		case *ast.SelectStmt:
			for _, c := range s.Body.List {
				cc := c.(*ast.CommClause)
				forEachStmt(cc.Body, onLast)
			}
		default:
			onLast(s)
		}
	}
}

// litStmts returns all statements from the function body of a function
// literal.
//
// If fun is not a function literal, it returns nil.
func litStmts(fun ast.Expr) []ast.Stmt {
	lit, _ := fun.(*ast.FuncLit)
	if lit == nil {
		return nil
	}
	return lit.Body.List
}

// goInvoke returns a function expression that would be called asynchronously
// (but not awaited) in another goroutine as a consequence of the call.
// For example, given the g.Go call below, it returns the function literal expression.
//
//	import "sync/errgroup"
//	var g errgroup.Group
//	g.Go(func() error { ... })
//
// Currently only "golang.org/x/sync/errgroup.Group()" is considered.
func goInvoke(info *types.Info, call *ast.CallExpr) ast.Expr {
	if !isMethodCall(info, call, "golang.org/x/sync/errgroup", "Group", "Go") {
		return nil
	}
	return call.Args[0]
}

// parallelSubtest returns statements that can be easily proven to execute
// concurrently via the go test runner, as t.Run has been invoked with a
// function literal that calls t.Parallel.
//
// In practice, users rely on the fact that statements before the call to
// t.Parallel are synchronous. For example by declaring test := test inside the
// function literal, but before the call to t.Parallel.
//
// Therefore, we only flag references in statements that are obviously
// dominated by a call to t.Parallel. As a simple heuristic, we only consider
// statements following the final labeled statement in the function body, to
// avoid scenarios where a jump would cause either the call to t.Parallel or
// the problematic reference to be skipped.
//
//	import "testing"
//
//	func TestFoo(t *testing.T) {
//		tests := []int{0, 1, 2}
//		for i, test := range tests {
//			t.Run("subtest", func(t *testing.T) {
//				println(i, test) // OK
//		 		t.Parallel()
//				println(i, test) // Not OK
//			})
//		}
//	}
func parallelSubtest(info *types.Info, call *ast.CallExpr) []ast.Stmt {
	if !isMethodCall(info, call, "testing", "T", "Run") {
		return nil
	}

	if len(call.Args) != 2 {
		// Ignore calls such as t.Run(fn()).
		return nil
	}

	lit, _ := call.Args[1].(*ast.FuncLit)
	if lit == nil {
		return nil
	}

	// Capture the *testing.T object for the first argument to the function
	// literal.
	if len(lit.Type.Params.List[0].Names) == 0 {
		return nil
	}

	tObj := info.Defs[lit.Type.Params.List[0].Names[0]]
	if tObj == nil {
		return nil
	}

	// Match statements that occur after a call to t.Parallel following the final
	// labeled statement in the function body.
	//
	// We iterate over lit.Body.List to have a simple, fast and "frequent enough"
	// dominance relationship for t.Parallel(): lit.Body.List[i] dominates
	// lit.Body.List[j] for i < j unless there is a jump.
	var stmts []ast.Stmt
	afterParallel := false
	for _, stmt := range lit.Body.List {
		stmt, labeled := unlabel(stmt)
		if labeled {
			// Reset: naively we don't know if a jump could have caused the
			// previously considered statements to be skipped.
			stmts = nil
			afterParallel = false
		}

		if afterParallel {
			stmts = append(stmts, stmt)
			continue
		}

		// Check if stmt is a call to t.Parallel(), for the correct t.
		exprStmt, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		expr := exprStmt.X
		if isMethodCall(info, expr, "testing", "T", "Parallel") {
			call, _ := expr.(*ast.CallExpr)
			if call == nil {
				continue
			}
			x, _ := call.Fun.(*ast.SelectorExpr)
			if x == nil {
				continue
			}
			id, _ := x.X.(*ast.Ident)
			if id == nil {
				continue
			}
			if info.Uses[id] == tObj {
				afterParallel = true
			}
		}
	}

	return stmts
}

// unlabel returns the inner statement for the possibly labeled statement stmt,
// stripping any (possibly nested) *ast.LabeledStmt wrapper.
//
// The second result reports whether stmt was an *ast.LabeledStmt.
func unlabel(stmt ast.Stmt) (ast.Stmt, bool) {
	labeled := false
	for {
		labelStmt, ok := stmt.(*ast.LabeledStmt)
		if !ok {
			return stmt, labeled
		}
		labeled = true
		stmt = labelStmt.Stmt
	}
}

// isMethodCall reports whether expr is a method call of
// <pkgPath>.<typeName>.<method>.
func isMethodCall(info *types.Info, expr ast.Expr, pkgPath, typeName, method string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}

	// Check that we are calling a method <method>
	f := typeutil.StaticCallee(info, call)
	if f == nil || f.Name() != method {
		return false
	}
	recv := f.Type().(*types.Signature).Recv()
	if recv == nil {
		return false
	}

	return false
}

func main() {
	singlechecker.Main(NewAnalyzer())
}
