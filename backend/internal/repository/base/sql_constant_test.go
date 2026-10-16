// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sqlTextArg is the position of the SQL text in each GORM method that takes
// some: a Where clause, a column list, an ORDER BY, a table name, a raw
// statement, or the inline condition of First/Find/Delete.
var sqlTextArg = map[string]int{
	"Where": 0, "Or": 0, "Not": 0, "Having": 0, "Order": 0, "Group": 0,
	"Select": 0, "Distinct": 0, "Omit": 0, "Joins": 0, "InnerJoins": 0,
	"Table": 0, "Raw": 0, "Exec": 0, "Pluck": 0, "Update": 0, "UpdateColumn": 0,
	"Expr":  0,
	"First": 1, "Find": 1, "Take": 1, "Last": 1, "Delete": 1,
}

// dynamicSQL names the functions that build SQL text at run time, and why
// it is safe: every fragment is one of our own constants, never a value from
// a caller. Values always go through a ? placeholder.
var dynamicSQL = map[string]string{
	"ListTasks":                        "the per-database metadata filter and sort order, and an ORDER BY picked from constants by the status filter",
	"GetNextTask":                      "the per-database sort order",
	"GetWorkspaceTaskCountsByCategory": "the per-database metadata filter",
	"syncNeedsInput":                   "pendingRequestClause, a per-database constant",
	"detailedStats":                    "the per-database date buckets, and a scope its two callers pass as a literal",
	"telemetryCounts":                  "a rollup table name its callers pass as a literal",
	"moveTelemetry":                    "the rollup table names in telemetryRollupTables",
	"ListTaskLatencyRollups":           "a table from taskLatencyRollupTable",
	"mergeTaskLatencyRollups":          "latency table constants from its two callers",
	"writeTaskLatencyRollups":          "latency table constants from every caller",
	"moveTaskLatencyRollups":           "the three latency table constants",
}

// TestSQLTextIsConstant fails when SQL text reaching GORM is not a
// compile-time constant outside the functions in dynamicSQL. Request input
// pasted into a query is SQL injection; a value must go through ? instead.
func TestSQLTextIsConstant(t *testing.T) {
	fset := token.NewFileSet()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}

	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{Importer: stdlibOnly{importer.Default()}, Error: func(error) {}}
	_, _ = conf.Check("base", fset, files, info)

	var found []string
	used := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				i, ok := sqlTextArg[sel.Sel.Name]
				if !ok || i >= len(call.Args) {
					return true
				}
				if constantOrNotText(info, call.Args[i]) {
					return true
				}
				if _, ok := dynamicSQL[fn.Name.Name]; ok {
					used[fn.Name.Name] = true
					return true
				}
				found = append(found, fset.Position(call.Args[i].Pos()).String()+" in "+fn.Name.Name+": "+sel.Sel.Name)
				return true
			})
		}
	}
	sort.Strings(found)
	for _, s := range found {
		t.Errorf("SQL text is built at run time at %s; pass values with ? instead, or, if every fragment is our own constant, add the function to dynamicSQL with the reason", s)
	}
	for name := range dynamicSQL {
		if !used[name] {
			t.Errorf("dynamicSQL lists %s, which no longer builds SQL text at run time; remove it", name)
		}
	}
}

// constantOrNotText reports whether e is a compile-time constant, or is not
// text at all (a model, a subquery, a column slice).
func constantOrNotText(info *types.Info, e ast.Expr) bool {
	tv, ok := info.Types[e]
	if ok && tv.Value != nil {
		return true
	}
	if ok && tv.Type != nil && tv.Type != types.Typ[types.Invalid] {
		b, isBasic := tv.Type.Underlying().(*types.Basic)
		return !isBasic || b.Info()&types.IsString == 0
	}
	// The type is unknown, because it comes from a package this test does
	// not load. Text joined with + is still text.
	b, ok := e.(*ast.BinaryExpr)
	return !ok || b.Op != token.ADD
}

// stdlibOnly loads the standard library, and gives every other import an
// empty package: the check needs the types of this package's own constants
// and of fmt.Sprintf, not those of GORM.
type stdlibOnly struct{ std types.Importer }

func (s stdlibOnly) Import(path string) (*types.Package, error) {
	if !strings.Contains(strings.Split(path, "/")[0], ".") {
		return s.std.Import(path)
	}
	name := path[strings.LastIndex(path, "/")+1:]
	p := types.NewPackage(path, name)
	p.MarkComplete()
	return p, nil
}
