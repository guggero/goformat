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
// Scope: var specifications with no values (no `var x = 1` assignment),
// including declarations collected into a block by R14. Only overlong lines
// are split.
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
		if ctx.SkipFormatting(n) {
			return false
		}
		gd, ok := n.(*dst.GenDecl)
		if !ok || gd.Tok != token.VAR {
			return true
		}
		var specs []dst.Spec
		for _, original := range gd.Specs {
			spec := original.(*dst.ValueSpec)
			astSpec, ok := ctx.Decorator.Ast.Nodes[spec].(*ast.ValueSpec)
			if !ok || ctx.SkipFormatting(
				spec,
			) || len(
				spec.Names,
			) < 2 || len(
				spec.Values,
			) > 0 || spec.Type == nil || !isSingleLine(
				ctx.FileSet, astSpec.Type.Pos(),
				astSpec.Type.End(),
			) || sourceLineWidth(
				ctx.FileSet, ctx.SourceLines,
				astSpec.Pos(), tab,
			) <= limit {

				specs = append(specs, spec)
				continue
			}
			indent := lineIndentAt(
				ctx.FileSet, ctx.SourceLines, astSpec.Pos(),
				tab,
			)
			if !gd.Lparen {
				indent += tab
			}
			wrapped := wrapVarSpec(
				ctx, spec, astSpec, limit, tab, indent,
			)
			if len(wrapped) > 1 {
				gd.Lparen = true
			}
			specs = append(specs, wrapped...)
		}
		gd.Specs = specs
		return true
	})
	return nil
}

func wrapVarSpec(ctx *Context, spec *dst.ValueSpec,
	astSpec *ast.ValueSpec, limit, tab, indent int) []dst.Spec {

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
	budget := limit - indent - 1 - typeWidth
	if budget < nameWidths[0] {
		// Even one name + type won't fit. Wrap anyway; R10 will warn.
		budget = nameWidths[0]
	}

	groups := packNamesIntoLines(nameWidths, budget)
	if len(groups) < 2 {
		return []dst.Spec{spec}
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
			typExpr = dst.Clone(spec.Type).(dst.Expr)
		}
		ns := &dst.ValueSpec{Names: names, Type: typExpr}
		newSpecs = append(newSpecs, ns)
		start = end
	}

	first := newSpecs[0].(*dst.ValueSpec)
	last := newSpecs[len(newSpecs)-1].(*dst.ValueSpec)
	first.Decs = spec.Decs
	first.Decs.End = nil
	first.Decs.After = dst.None
	last.Decs.End = spec.Decs.End
	last.Decs.After = spec.Decs.After
	return newSpecs
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
