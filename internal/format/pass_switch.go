package format

import (
	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// switchCaseSpacing implements R1: a blank line between consecutive case
// clauses of a switch / type-switch / select statement.
type switchCaseSpacing struct{}

func (switchCaseSpacing) Name() string { return "R1" }

func (switchCaseSpacing) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.SwitchCaseSpacingOn() {
		return nil
	}
	dst.Inspect(ctx.File, func(n dst.Node) bool {
		if ctx.SkipNolintDecl(n) {
			return false
		}
		switch s := n.(type) {
		case *dst.SwitchStmt:
			spaceClauses(s.Body.List)

		case *dst.TypeSwitchStmt:
			spaceClauses(s.Body.List)

		case *dst.SelectStmt:
			spaceClauses(s.Body.List)
		}
		return true
	})
	return nil
}

// spaceClauses sets an empty line before every clause after the first. Setting
// Before to EmptyLine is idempotent — running twice produces the same output.
func spaceClauses(clauses []dst.Stmt) {
	if len(clauses) < 2 {
		return
	}
	for i := 1; i < len(clauses); i++ {
		switch c := clauses[i].(type) {
		case *dst.CaseClause:
			c.Decs.Before = dst.EmptyLine

		case *dst.CommClause:
			c.Decs.Before = dst.EmptyLine
		}
	}
}
