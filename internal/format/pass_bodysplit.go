package format

import (
	"go/ast"
	"go/token"

	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// bodySplit implements R12: when a function body fits on the same line as its
// opening brace (`func F(args) T { body }`) and that line exceeds the
// configured limit, force the body onto its own indented lines so each
// individual line satisfies the limit.
//
// Detection looks at the source: lbrace.Line == rbrace.Line AND the containing
// source line is over the limit. Mutation: set Before=NewLine on the first body
// statement. gofmt then promotes the whole body to multi-line and the closing
// `}` follows on its own line.
//
// FuncLit (function literals as expressions) gets the same treatment — both
// reach this pass via dst.Inspect.
type bodySplit struct{}

func (bodySplit) Name() string { return "R12" }

func (bodySplit) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.BodySplitOn() {
		return nil
	}
	limit := ctx.Config.LineLength
	tab := ctx.Config.TabWidth
	if tab <= 0 {
		tab = 8
	}

	dst.Inspect(ctx.File, func(n dst.Node) bool {
		switch fn := n.(type) {
		case *dst.FuncDecl:
			astN, ok := ctx.Decorator.Ast.Nodes[fn]
			if !ok {
				return true
			}
			astFD := astN.(*ast.FuncDecl)
			if astFD.Body == nil {
				return true
			}
			maybeSplit(
				ctx, fn.Body, astFD.Body.Lbrace,
				astFD.Body.Rbrace, limit, tab,
			)

		case *dst.FuncLit:
			astN, ok := ctx.Decorator.Ast.Nodes[fn]
			if !ok {
				return true
			}
			astFL := astN.(*ast.FuncLit)
			if astFL.Body == nil {
				return true
			}
			maybeSplit(
				ctx, fn.Body, astFL.Body.Lbrace,
				astFL.Body.Rbrace, limit, tab,
			)
		}
		return true
	})
	return nil
}

func maybeSplit(ctx *Context, body *dst.BlockStmt, lbrace, rbrace token.Pos,
	limit, tab int) {

	if body == nil || len(body.List) == 0 {
		return
	}
	if !isSingleLine(ctx.FileSet, lbrace, rbrace) {
		return
	}
	if sourceLineWidth(ctx.FileSet, ctx.SourceLines, lbrace, tab) <= limit {
		return
	}
	body.List[0].Decorations().Before = dst.NewLine

	// Mark every call inside the now-split body as OuterHandled. R4
	// measures call positions from source columns, and source put every
	// inner call on the (over-limit) single-line body line. R4 would treat
	// those source columns as authoritative and wrap each inner call —
	// even though after the split they sit at a perfectly normal body
	// indent.
	for _, stmt := range body.List {
		dst.Inspect(stmt, func(n dst.Node) bool {
			if c, ok := n.(*dst.CallExpr); ok {
				ctx.OuterHandled[c] = true
			}
			return true
		})
	}
}
