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

// dynamicSQL names the exact expressions that build SQL text at run time in
// each function, and why they are safe: every fragment is one of our own
// constants, never a value from a caller. Values always go through a ?
// placeholder.
var dynamicSQL = map[string]string{
	`ListTasks:"id IN (SELECT task_id FROM messages m1 WHERE created_at = (SELECT MAX(created_at) FROM messages m2 WHERE m2.task_id = m1.task_id) AND " + metadataExpr + ")"`: "the per-database metadata filter",
	`ListTasks:orderBy`: "the per-database sort order and an ORDER BY picked from constants by the status filter",
	`ListTasks:"id = (SELECT MAX(id) FROM messages m2 WHERE m2.task_id = messages.task_id) OR (" + metadataExpr + ")"`:                                                                               "the per-database metadata filter",
	`GetNextTask:fmt.Sprintf("%s ASC, id ASC", sortExpr)`:                                                                                                                                            "the per-database sort order",
	`GetWorkspaceTaskCountsByCategory:"id IN (SELECT task_id FROM messages m1 WHERE created_at = (SELECT MAX(created_at) FROM messages m2 WHERE m2.task_id = m1.task_id) AND " + metadataExpr + ")"`: "the per-database metadata filter",
	`syncNeedsInput:pendingRequestClause(tx)`:                    "pendingRequestClause, a per-database constant",
	`detailedStats:scopeClause`:                                  "a scope its two callers pass as a literal",
	`detailedStats:dateExpr + " as date, count(*) as count"`:     "the per-database date buckets",
	`detailedStats:bucketExpr + " as bucket, count(*) as count"`: "the per-database date buckets",
	`telemetryCounts:table`:                                      "a rollup table name its callers pass as a literal",
	`moveTelemetry:"INSERT INTO " + table + " (period_start, user_id, workspace_id, action, sub_action_id, actor, count) " + "SELECT period_start, user_id, ?, action, sub_action_id, actor, count FROM " + table + " WHERE workspace_id = ? " + "ON CONFLICT (period_start, user_id, workspace_id, action, sub_action_id, actor) " + "DO UPDATE SET count = " + table + ".count + excluded.count"`: "the rollup table names in telemetryRollupTables",
	`moveTelemetry:"DELETE FROM " + table + " WHERE workspace_id = ?"`:          "the rollup table names in telemetryRollupTables",
	`ListTaskLatencyRollups:taskLatencyRollupTable[g]`:                          "a table from taskLatencyRollupTable",
	`mergeTaskLatencyRollups:from`:                                              "latency table constants from its two callers",
	`writeTaskLatencyRollups:table`:                                             "latency table constants from every caller",
	`moveTaskLatencyRollups:table`:                                              "the three latency table constants",
	`moveTaskLatencyRollups:"DELETE FROM " + table + " WHERE workspace_id = ?"`: "the three latency table constants",
}

// TestSQLTextIsConstant fails when SQL text reaching GORM is not a
// compile-time constant outside the expressions in dynamicSQL. Request input
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

	found, used := checkSQLConstants(fset, files)
	for _, s := range found {
		t.Errorf("SQL text is built at run time at %s; pass values with ? instead, or, if every fragment is our own constant, add the expression to dynamicSQL with the reason", s)
	}
	for key := range dynamicSQL {
		if !used[key] {
			t.Errorf("dynamicSQL lists %s, which no longer builds SQL text at run time; remove it", key)
		}
	}
}

func checkSQLConstants(fset *token.FileSet, files []*ast.File) (found []string, used map[string]bool) {
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{Importer: stdlibOnly{importer.Default()}, Error: func(error) {}}
	_, _ = conf.Check("base", fset, files, info)

	used = map[string]bool{}
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
				key := fn.Name.Name + ":" + types.ExprString(call.Args[i])
				if _, ok := dynamicSQL[key]; ok {
					used[key] = true
					return true
				}
				found = append(found, fset.Position(call.Args[i].Pos()).String()+" in "+fn.Name.Name+": "+sel.Sel.Name+"("+types.ExprString(call.Args[i])+")")
				return true
			})
		}
	}
	sort.Strings(found)
	return found, used
}

// constantOrNotText reports whether e is a compile-time constant, or is not
// text at all (a model pointer/struct or a typed non-string value).
func constantOrNotText(info *types.Info, e ast.Expr) bool {
	tv, ok := info.Types[e]
	if ok && tv.Value != nil {
		return true
	}
	if ok && tv.Type != nil && tv.Type != types.Typ[types.Invalid] {
		b, isBasic := tv.Type.Underlying().(*types.Basic)
		return !isBasic || b.Info()&types.IsString == 0
	}
	// When the type comes from an unloaded package (such as entity, model, or
	// clause), only struct composite literals and clause.Associations are
	// allowed; struct fields like req.Filter or concatenated strings must be
	// treated as runtime text.
	switch x := e.(type) {
	case *ast.CompositeLit:
		return true
	case *ast.UnaryExpr:
		_, isLit := x.X.(*ast.CompositeLit)
		return x.Op == token.AND && isLit
	default:
		return types.ExprString(e) == "clause.Associations"
	}
}

func TestSQLTextIsConstant_RejectsInjectedSQL(t *testing.T) {
	const badSrc = `package base
import (
	"fmt"
	"github.com/agentrq/agentrq/backend/internal/data/entity"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)
func (r *repository) badQueries(req entity.ListTasksRequest, tc model.ToolCall, col string) {
	r.conn(nil).Where("title = '" + req.Filter + "'")
	r.conn(nil).Where(fmt.Sprintf("status = '%s'", col))
	r.conn(nil).Order(req.Filter)
	r.conn(nil).Exec(tc.ToolName)
	r.conn(nil).First(&tc, col)
	r.conn(nil).Where("title = ?", req.Filter).First(&model.Task{}, 1)
}
func (r *repository) ListTasks(req entity.ListTasksRequest) {
	r.conn(nil).Where("injected = " + req.Assignee)
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "bad.go", badSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := checkSQLConstants(fset, []*ast.File{f})
	if len(found) != 6 {
		t.Fatalf("checkSQLConstants caught %d of 6 injected queries: %v", len(found), found)
	}
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
