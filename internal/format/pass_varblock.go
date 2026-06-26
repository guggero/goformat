package format

import (
	"go/ast"
	"go/token"

	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// varBlockWrap implements R13: a single-line `var a, b, c, ... T` declaration
// whose line exceeds the limit gets rewritten as a grouped `var ( ... )` block,
// with the names packed across spec lines.
//
// Example:
//
//	var argument1, argument2, argument3, argument4, argument5, argument6,
//
// argument7 int
//
// becomes:
//
//	var (
//	    argument1, argument2, argument3, argument4, argument5, argument6 int
//	    argument7                                                        int
//	)
//
// gofmt auto-aligns the trailing types across the spec lines.
//
// Scope: only var; only ungrouped specs with a single ValueSpec and no values
// (no `var x = 1` assignment); only when the original line is over the limit.
// const/type declarations and value-bearing vars are out of scope for v1.
type varBlockWrap struct{}

func (varBlockWrap) Name() string { return "R13" }

func (varBlockWrap) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.VarBlockWrapOn() {
		return nil
	}
	limit := ctx.Config.LineLength
	tab := ctx.Config.TabWidth
	if tab <= 0 {
		tab = 8
	}

	dst.Inspect(ctx.File, func(n dst.Node) bool {
		if ctx.SkipNolintDecl(n) {
			return false
		}
		gd, ok := n.(*dst.GenDecl)
		if !ok || gd.Tok != token.VAR || gd.Lparen {
			return true
		}
		if len(gd.Specs) != 1 {
			return true
		}
		spec, ok := gd.Specs[0].(*dst.ValueSpec)
		if !ok {
			return true
		}
		if len(spec.Names) < 2 || len(spec.Values) > 0 ||
			spec.Type == nil {

			return true
		}
		astN, ok := ctx.Decorator.Ast.Nodes[gd]
		if !ok {
			return true
		}
		astGD := astN.(*ast.GenDecl)
		astSpec := astGD.Specs[0].(*ast.ValueSpec)

		if sourceLineWidth(
			ctx.FileSet, ctx.SourceLines, astGD.Pos(), tab,
		) <= limit {

			return true
		}

		applyVarBlockWrap(ctx, gd, spec, astSpec, limit, tab)
		return true
	})
	return nil
}

func applyVarBlockWrap(ctx *Context, gd *dst.GenDecl, spec *dst.ValueSpec,
	astSpec *ast.ValueSpec, limit, tab int) {

	nameWidths := make([]int, len(spec.Names))
	for i, n := range spec.Names {
		nameWidths[i] = len(n.Name)
	}
	typeWidth := sourceWidth(
		ctx.FileSet, ctx.SourceLines, astSpec.Type.Pos(),
		astSpec.Type.End(), tab,
	)
	if typeWidth <= 0 {
		typeWidth = 3 // conservative fallback
	}

	// Per spec line: indent + names + (k-1)*", " + " " + type. Budget for
	// names + separators: limit - tab - 1 (space) - type.
	budget := limit - tab - 1 - typeWidth
	if budget < nameWidths[0] {
		// Even one name + type won't fit. Wrap anyway; R10 will warn.
		budget = nameWidths[0]
	}

	groups := packNamesIntoLines(nameWidths, budget)
	if len(groups) < 2 {
		return // packed onto a single line — no improvement.
	}

	newSpecs := make([]dst.Spec, 0, len(groups))
	start := 0
	for gi, end := range groups {
		names := spec.Names[start:end]
		var typExpr dst.Expr
		if gi == 0 {
			// Reuse the original type node on the first spec.
			typExpr = spec.Type
		} else {
			typExpr = cloneTypeIdent(spec.Type)
		}
		ns := &dst.ValueSpec{Names: names, Type: typExpr}
		newSpecs = append(newSpecs, ns)
		start = end
	}

	gd.Specs = newSpecs
	gd.Lparen = true
}

// packNamesIntoLines greedy-packs name widths into groups such that each
// group's `name1, name2, ...` text fits within budget. Returns the index AFTER
// the last name in each group (i.e. a sequence of break-end indices ending with
// len(widths)).
func packNamesIntoLines(widths []int, budget int) []int {
	if len(widths) == 0 {
		return nil
	}
	const sep = 2 // ", "
	var ends []int
	lineWidth := widths[0]
	lineStart := 0
	for i := 1; i < len(widths); i++ {
		prospective := lineWidth + sep + widths[i]
		if prospective > budget {
			ends = append(ends, i)
			lineWidth = widths[i]
			lineStart = i
		} else {
			lineWidth = prospective
		}
	}
	ends = append(ends, len(widths))
	_ = lineStart
	return ends
}

// cloneTypeIdent returns a shallow copy of a type expression suitable for use
// in a fresh ValueSpec. dst nodes are pointers, so we'd otherwise be sharing
// the same instance across specs — generally fine for printing, but the safer
// move is to give each spec its own. We handle the common shapes (Ident,
// StarExpr-of-Ident, SelectorExpr); for anything else, we fall back to sharing
// the original — worst-case correctness is the same since dst's printer is a
// pure read of the tree.
func cloneTypeIdent(t dst.Expr) dst.Expr {
	switch x := t.(type) {
	case *dst.Ident:
		return &dst.Ident{Name: x.Name}

	case *dst.StarExpr:
		return &dst.StarExpr{X: cloneTypeIdent(x.X)}

	case *dst.SelectorExpr:
		if id, ok := x.X.(*dst.Ident); ok {
			return &dst.SelectorExpr{
				X:   &dst.Ident{Name: id.Name},
				Sel: &dst.Ident{Name: x.Sel.Name},
			}
		}

	case *dst.ArrayType:
		return &dst.ArrayType{Elt: cloneTypeIdent(x.Elt)}
	}
	return t
}
