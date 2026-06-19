package format

import (
	"go/ast"
	"go/token"

	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// structuredLogWrap implements R8: methods like log.InfoS(ctx, "msg", attrs...)
// get their own layout — attrs each on their own line, close paren attached
// to the last attr (not on its own line as R4 would do). R8 also lints: if the
// msg arg isn't a static string literal, emit a warning.
//
// Runs on every CallExpr. Detection is name-based (exact method-name match
// against cfg.StructuredLogMethods); typed detection via go/packages is a
// post-v0.1 refinement.
type structuredLogWrap struct{}

func (structuredLogWrap) Name() string { return "R8" }

func (structuredLogWrap) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.StructuredLogWrapOn() {
		return nil
	}
	allowed := ctx.Config.StructuredLogMethods
	if len(allowed) == 0 {
		return nil
	}
	limit := ctx.Config.LineLength
	tab := ctx.Config.TabWidth
	if tab <= 0 {
		tab = 8
	}

	var diags []diag.Diagnostic
	dst.Inspect(ctx.File, func(n dst.Node) bool {
		call, ok := n.(*dst.CallExpr)
		if !ok {
			return true
		}
		astN, ok := ctx.Decorator.Ast.Nodes[call]
		if !ok {
			return true
		}
		astCall := astN.(*ast.CallExpr)
		if !isStructuredLogCall(astCall, allowed) {
			return true
		}

		if d := lintStructuredLogMsg(ctx, astCall); d != nil {
			diags = append(diags, *d)
		}

		applyStructuredLogLayout(ctx, astCall, call, limit, tab)
		return true
	})
	return diags
}

func isStructuredLogCall(call *ast.CallExpr, allowed []string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if !inStringSet(sel.Sel.Name, allowed) {
		return false
	}
	if len(call.Args) < 2 {
		return false
	}
	switch call.Args[0].(type) {
	case *ast.Ident, *ast.SelectorExpr:
		return true
	}
	return false
}

func lintStructuredLogMsg(ctx *Context, call *ast.CallExpr) *diag.Diagnostic {
	if len(call.Args) < 2 {
		return nil
	}
	lit, ok := call.Args[1].(*ast.BasicLit)
	if ok && lit.Kind == token.STRING {
		return nil
	}
	pos := ctx.FileSet.Position(call.Args[1].Pos())
	return &diag.Diagnostic{
		Rule:     "R8",
		Severity: diag.Warn,
		File:     ctx.Filename,
		Line:     pos.Line,
		Col:      pos.Column,
		Message: "structured-log msg should be a static string " +
			"literal",
	}
}

// applyStructuredLogLayout decides between single-line and "one attr per line"
// for the call, then stamps the decision. Args[0] (ctx) and Args[1] (msg)
// always stay on the call line; args[2:] are the attrs that may split.
func applyStructuredLogLayout(ctx *Context, astCall *ast.CallExpr,
	call *dst.CallExpr, limit, tab int) {

	clearArgDecorations(call.Args)
	if len(call.Args) < 3 {
		return
	}
	fset := ctx.FileSet
	lines := ctx.SourceLines

	callCol := visualCol(fset, lines, astCall.Pos(), tab)
	calleeW := sourceWidth(
		fset, lines, astCall.Fun.Pos(), astCall.Fun.End(), tab,
	)
	widths := argWidths(fset, lines, astCall.Args, tab)
	sumW := sum(widths)
	seps := (len(widths) - 1) * 2

	singleLine := callCol + calleeW + 1 + sumW + seps + 1
	if singleLine <= limit {
		return // fits inline; leave it on one line
	}
	for i := 2; i < len(call.Args); i++ {
		call.Args[i].Decorations().Before = dst.NewLine
	}

	// Mark inner CallExpr args so R4 doesn't re-wrap them at their stale
	// source column.
	markInnerCallsHandled(ctx.OuterHandled, call)
}
