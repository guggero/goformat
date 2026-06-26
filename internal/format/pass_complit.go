package format

import (
	"go/ast"

	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// compositeLitReflow implements R7: a composite literal whose source line
// exceeds the limit gets reflowed onto multiple lines.
//
//   - Keyed composites (struct & map literals — elts are KeyValueExpr)
//     get one element per line. The doc forbids packing struct fields
//     across lines because it ruins git-diff hygiene.
//   - Non-keyed composites (array / slice literals — elts are bare
//     values) get packed like function arguments: greedy fill on each
//     line up to the limit.
//
// R7 runs BEFORE R4 so the call-wrap pass sees the reflowed literal as a
// multi-line container and can apply the inline-symmetric form (closing `}` and
// outer `)` on a shared closing line) when it fits.
type compositeLitReflow struct{}

func (compositeLitReflow) Name() string { return "R7" }

func (compositeLitReflow) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.InlineCompositeLitOn() {
		return nil
	}
	tab := ctx.Config.TabWidth
	if tab <= 0 {
		tab = 8
	}
	limit := ctx.Config.LineLength
	if limit <= 0 {
		limit = 80
	}

	dst.Inspect(ctx.File, func(n dst.Node) bool {
		if ctx.SkipNolintDecl(n) {
			return false
		}
		comp, ok := n.(*dst.CompositeLit)
		if !ok || len(comp.Elts) == 0 {
			return true
		}
		astN, ok := ctx.Decorator.Ast.Nodes[comp]
		if !ok {
			return true
		}
		astComp, ok := astN.(*ast.CompositeLit)
		if !ok {
			return true
		}

		// Already multi-line in source — leave it; the developer has
		// chosen a layout and reflowing would just churn.
		if !isSingleLine(ctx.FileSet, astComp.Lbrace, astComp.Rbrace) {
			return true
		}

		// Source line containing the literal fits — nothing to do.
		if sourceLineWidth(
			ctx.FileSet, ctx.SourceLines, astComp.Pos(), tab,
		) <= limit {

			return true
		}

		if isKeyedComposite(comp) {
			applyStructReflow(comp)
		} else {
			applySliceReflow(ctx, astComp, comp, tab, limit)
		}
		return true
	})
	return nil
}

// anyCompositeLineOverLimit returns true if any source line in the composite
// literal's span exceeds limit. Walks from the opening "{" to the closing "}"
// — for single-line composites that's one line, for multi-line composites
// with packed elts it catches the over-limit inner line.
func anyCompositeLineOverLimit(ctx *Context, comp *ast.CompositeLit,
	limit, tab int) bool {

	firstLine := ctx.FileSet.Position(comp.Lbrace).Line
	lastLine := ctx.FileSet.Position(comp.Rbrace).Line
	for ln := firstLine; ln <= lastLine; ln++ {
		if ln <= 0 || ln > len(ctx.SourceLines) {
			continue
		}
		if visualWidth(ctx.SourceLines[ln-1], tab) > limit {
			return true
		}
	}
	return false
}

// isKeyedComposite reports whether the literal's elements are KeyValueExpr
// (struct or map). The first element decides — Go won't let you mix keyed and
// unkeyed in one literal.
func isKeyedComposite(comp *dst.CompositeLit) bool {
	_, isKey := comp.Elts[0].(*dst.KeyValueExpr)
	return isKey
}

// applyStructReflow lays out a keyed composite literal with one field per line.
// The `{` stays on the previous token's line; the `}` goes on its own line at
// the literal's indent. gofmt's printer takes the rest from the NewLine markers
// we stamp.
func applyStructReflow(comp *dst.CompositeLit) {
	for _, e := range comp.Elts {
		e.Decorations().Before = dst.NewLine
	}
	comp.Elts[len(comp.Elts)-1].Decorations().After = dst.NewLine
}

// applySliceReflow lays out a non-keyed composite literal with elements packed
// like function arguments — greedy fill on each line up to the limit.
// Continuation indent is the literal's source-line indent plus one tab.
func applySliceReflow(ctx *Context, astComp *ast.CompositeLit,
	comp *dst.CompositeLit, tab, limit int) {

	widths := argWidths(ctx.FileSet, ctx.SourceLines, astComp.Elts, tab)
	contIndent := lineIndentAt(
		ctx.FileSet, ctx.SourceLines, astComp.Pos(), tab,
	) + tab
	contBudget := limit - contIndent
	breaks := packLayout(widths, contBudget, contBudget, 1)

	clearArgDecorations(comp.Elts)
	comp.Elts[0].Decorations().Before = dst.NewLine
	for _, i := range breaks {
		if i >= 0 && i < len(comp.Elts) {
			comp.Elts[i].Decorations().Before = dst.NewLine
		}
	}
	comp.Elts[len(comp.Elts)-1].Decorations().After = dst.NewLine
}
