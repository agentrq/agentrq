// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

// dynamicSQL names each fragment that puts SQL text together at run time,
// as function:fragment, and why it is safe: the fragment is one of our own
// constants, never a value from a caller. Values always go through a ?
// placeholder. A fragment is a non-constant operand of a + or of
// fmt.Sprintf, or the whole argument when it is neither.
var dynamicSQL = map[string]string{
	"ListTasks:metadataExpr":                           "the per-database metadata filter",
	"ListTasks:orderBy":                                "the per-database sort order, and an ORDER BY picked from constants by the status filter",
	"GetNextTask:sortExpr":                             "the per-database sort order",
	"GetWorkspaceTaskCountsByCategory:metadataExpr":    "the per-database metadata filter",
	"syncNeedsInput:pendingRequestClause(tx)":          "pendingRequestClause, a per-database constant",
	"SummarizeTasks:pendingRequestClause(r.conn(ctx))": "pendingRequestClause, a per-database constant",
	"detailedStats:scopeClause":                        "a scope its two callers pass as a literal",
	"detailedStats:dateExpr":                           "the per-database date buckets",
	"detailedStats:bucketExpr":                         "the per-database date buckets",
	"telemetryCounts:table":                            "a rollup table name its callers pass as a literal",
	"moveTelemetry:table":                              "the rollup table names in telemetryRollupTables",
	"ListTaskLatencyRollups:taskLatencyRollupTable[g]": "a table from taskLatencyRollupTable",
	"mergeTaskLatencyRollups:from":                     "latency table constants from its two callers",
	"writeTaskLatencyRollups:table":                    "latency table constants from every caller",
	"moveTaskLatencyRollups:table":                     "the three latency table constants",
}

// TestSQLTextIsConstant fails when SQL text reaching GORM is not a
// compile-time constant outside the expressions in dynamicSQL. Request input
// pasted into a query is SQL injection; a value must go through ? instead.
func TestSQLTextIsConstant(t *testing.T) {
	fset, imp := sourceTypes()
	files := packageFiles(t, fset)
	found, used := checkSQLConstants(fset, imp, files)
	for _, f := range found {
		t.Errorf("SQL text is built at run time from %s at %s; pass values with ? instead, or, if it is our own constant, add %s to dynamicSQL with the reason", f.fragment, f.pos, f.key())
	}
	for key := range dynamicSQL {
		if !used[key] {
			t.Errorf("dynamicSQL lists %s, which no longer builds SQL text at run time; remove it", key)
		}
	}
}

// packageFiles parses the package's own non-test files.
func packageFiles(t *testing.T, fset *token.FileSet) []*ast.File {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return files
}

// sqlFinding is a fragment of SQL text built at run time that dynamicSQL
// does not list.
type sqlFinding struct {
	pos                  token.Position
	fn, method, fragment string
}

func (f sqlFinding) key() string { return f.fn + ":" + f.fragment }

// sourceTypes loads every import from source, so a model, a subquery or a
// column slice is known not to be text. The importer keeps what it loaded,
// so the tests share one and GORM is type-checked once.
var sourceTypes = sync.OnceValues(func() (*token.FileSet, types.Importer) {
	fset := token.NewFileSet()
	return fset, importer.ForCompiler(fset, "source", nil)
})

// checkSQLConstants type-checks files as package base and reports the SQL
// fragments built at run time.
func checkSQLConstants(fset *token.FileSet, imp types.Importer, files []*ast.File) (found []sqlFinding, used map[string]bool) {
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{Importer: imp, Error: func(error) {}}
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
				for _, part := range dynamicFragments(info, call.Args[i]) {
					finding := sqlFinding{fset.Position(part.Pos()), fn.Name.Name, sel.Sel.Name, types.ExprString(part)}
					if _, ok := dynamicSQL[finding.key()]; ok {
						used[finding.key()] = true
						continue
					}
					found = append(found, finding)
				}
				return true
			})
		}
	}
	return found, used
}

// dynamicFragments splits SQL text into the parts not known when it is
// compiled: the non-constant operands of a + or of fmt.Sprintf, or e itself.
func dynamicFragments(info *types.Info, e ast.Expr) []ast.Expr {
	if constantOrNotText(info, e) {
		return nil
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return dynamicFragments(info, x.X)
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return append(dynamicFragments(info, x.X), dynamicFragments(info, x.Y)...)
		}
	case *ast.CallExpr:
		if types.ExprString(x.Fun) == "fmt.Sprintf" && len(x.Args) > 0 && constantOrNotText(info, x.Args[0]) {
			var parts []ast.Expr
			for _, a := range x.Args[1:] {
				parts = append(parts, dynamicFragments(info, a)...)
			}
			return parts
		}
	}
	return []ast.Expr{e}
}

// constantOrNotText reports whether e is a compile-time constant, or is not
// text at all (a model, a subquery, a column slice). An expression whose type
// could not be worked out is text unless it is a composite literal.
func constantOrNotText(info *types.Info, e ast.Expr) bool {
	tv, ok := info.Types[e]
	if ok && tv.Value != nil {
		return true
	}
	if ok && tv.Type != nil && tv.Type != types.Typ[types.Invalid] {
		b, isBasic := tv.Type.Underlying().(*types.Basic)
		return !isBasic || b.Info()&types.IsString == 0
	}
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	_, isLit := e.(*ast.CompositeLit)
	return isLit
}

// TestSQLTextIsConstant_RejectsInjectedSQL checks the guard against the
// package's own files plus one more: every injection in it is reported, on its
// line, and nothing that is safe is.
func TestSQLTextIsConstant_RejectsInjectedSQL(t *testing.T) {
	const badSrc = `package base

import (
	"fmt"

	"github.com/agentrq/agentrq/backend/internal/data/entity"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

func (r *repository) sqlProbe(req entity.ListTasksRequest, tc model.ToolCall, cond model.Task, col string) {
	var rows []model.Task
	sub := r.conn(nil).Model(&model.Message{}).Select("task_id")
	r.conn(nil).Where("title = '" + req.Filter + "'")
	r.conn(nil).Where(fmt.Sprintf("status = '%s'", col))
	r.conn(nil).Order(req.Filter)
	r.conn(nil).Exec(tc.ToolName)
	r.conn(nil).First(&tc, col)
	r.conn(nil).Where("title = ?", req.Filter).First(&model.Task{}, 1)
	r.conn(nil).Where("id IN (?)", sub).Where(sub).Where(&cond).Find(&rows, cond)
	r.conn(nil).Where(fmt.Sprintf("LIMIT %d", len(rows)))
}

func ListTasks(r *repository, req entity.ListTasksRequest, metadataExpr string) {
	r.conn(nil).Where("a = " + metadataExpr + " AND b = " + req.Assignee)
}
`
	fset, imp := sourceTypes()
	files := packageFiles(t, fset)
	bad, err := parser.ParseFile(fset, "probe.go", badSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := checkSQLConstants(fset, imp, append(files, bad))
	var got []string
	for _, f := range found {
		got = append(got, fmt.Sprintf("%d %s %s(%s)", f.pos.Line, f.fn, f.method, f.fragment))
	}
	want := []string{
		"13 sqlProbe Where(req.Filter)",
		"14 sqlProbe Where(col)",
		"15 sqlProbe Order(req.Filter)",
		"16 sqlProbe Exec(tc.ToolName)",
		"17 sqlProbe First(col)",
		"24 ListTasks Where(req.Assignee)",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the guard reported\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
