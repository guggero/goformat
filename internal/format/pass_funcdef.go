package format

import (
	"go/ast"
	"go/token"

	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// funcDefWrap implements R3: greedily pack parameters and results, keeping
// closing parentheses attached and the body's opening brace on an indented
// continuation line whenever the signature spans multiple lines.
type funcDefWrap struct{}

func (funcDefWrap) Name() string { return "R3" }

func (funcDefWrap) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.FuncDefWrapOn() {
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
		fd, ok := n.(*dst.FuncDecl)
		if !ok {
			return true
		}
		if fd.Type == nil || fd.Type.Params == nil {
			return true
		}
		astN, ok := ctx.Decorator.Ast.Nodes[fd]
		if !ok {
			return true
		}
		astFD, ok := astN.(*ast.FuncDecl)
		if !ok || astFD.Body == nil {
			return true
		}

		// HARD-only by default: a signature is re-wrapped solely to
		// resolve an over-limit line. If every line the signature
		// occupies already fits, the author's layout is valid — leave
		// it. Repacking or collapsing a fitting signature is a SOFT,
		// --optimize-only change. (R2's body-blank still works:
		// MultilineSigs is seeded from the source state in analyse().)
		if !ctx.Config.Optimize &&
			sigLinesFit(ctx, astFD.Type, astFD.Body, limit, tab) {

			return true
		}

		fields, widths, seed, tail, ok := funcSignatureParts(
			ctx, astFD.Type, fd.Type, astFD.Body.Lbrace, tab,
		)
		if !ok {
			return true
		}
		indent := lineIndentAt(
			ctx.FileSet, ctx.SourceLines, astFD.Type.Func, tab,
		)
		breaks, ok := packFuncDef(
			widths, seed, tail, indent, indent+tab, limit,
		)
		if !ok {
			// Leave irreducible signatures alone; R10 reports them.
			return true
		}
		if !applyFuncSignatureLayout(fd.Type, fields, breaks) {
			return true
		}
		if len(breaks) == 0 {
			delete(ctx.MultilineSigs, fd)
		} else {
			ctx.MultilineSigs[fd] = true
		}
		return true
	})
	return nil
}

// sigLinesFit reports whether every source line the function signature occupies
// — from the `func` keyword through the opening body brace — is within the
// limit. When true, the signature's current layout is structurally valid and
// must be left untouched in the default (non-optimize) mode.
func sigLinesFit(ctx *Context, ft *ast.FuncType, body *ast.BlockStmt,
	limit, tab int) bool {

	fset := ctx.FileSet
	start := fset.Position(ft.Func).Line
	end := fset.Position(body.Lbrace).Line
	for ln := start; ln <= end; ln++ {
		if ln <= 0 || ln > len(ctx.SourceLines) {
			return false
		}
		if visualWidth(ctx.SourceLines[ln-1], tab) > limit {
			return false
		}
	}
	return true
}

// funcSignatureParts builds the units between legal signature break points. The
// first result stays attached to the final parameter: splitting between them
// would leave a dangling result opening parenthesis, forbidden by the style.
func funcSignatureParts(ctx *Context, af *ast.FuncType, df *dst.FuncType,
	lbrace token.Pos, tab int) (fields []*dst.Field, widths []int,
	seed, tail int, ok bool) {

	fset, lines := ctx.FileSet, ctx.SourceLines
	seed = sourceWidth(
		fset, lines, af.Func, af.Params.Opening+1, tab,
	)
	if seed >= wideForcedBreak {
		return
	}

	// Comments and multi-line types can impose their own line breaks.
	// Preserve those layouts rather than packing across them.
	for _, group := range ctx.AstFile.Comments {
		if group.Pos() > af.Func && group.Pos() < lbrace {
			return
		}
	}
	fields = append(fields, df.Params.List...)
	widths = fieldWidths(fset, lines, af.Params.List, tab)
	for _, w := range widths {
		if w >= wideForcedBreak {
			return
		}
	}

	tail = 3 // ") {"
	results := af.Results
	if results != nil && len(results.List) > 0 {
		rw := fieldWidths(fset, lines, results.List, tab)
		for _, w := range rw {
			if w >= wideForcedBreak {
				return
			}
		}
		join := 2 // ") "
		tail = 2  // " {"
		if results.Opening.IsValid() {
			join++ // "("
			tail++ // ")"
		}
		if len(widths) > 0 {
			widths[len(widths)-1] += join + rw[0]
		} else {
			// A fixed first unit keeps the first result on the func
			// line and gives later results their comma separator.
			fields = append(fields, nil)
			widths = append(widths, join+rw[0])
		}
		fields = append(fields, df.Results.List[1:]...)
		widths = append(widths, rw[1:]...)
	}
	ok = true
	return
}

// packFuncDef packs each line through the last legal break that fits. Widths
// include the parameter/result boundary, so results have the same priority as
// parameters. Unlike call packing, a break before the first parameter is legal.
func packFuncDef(widths []int,
	seed, tail, firstCol, contIndent, limit int) ([]int, bool) {

	line := firstCol + seed
	if line > limit {
		return nil, false
	}
	var breaks []int
	for i, w := range widths {
		sep := 2 // ", "
		if i == 0 {
			sep = 0
		}
		trail := 1 // comma at the end of a non-final line
		if i == len(widths)-1 {
			trail = tail
		}
		if line+sep+w+trail > limit {
			breaks = append(breaks, i)
			line = contIndent + w
		} else {
			line += sep + w
		}
		if line+trail > limit {
			return nil, false
		}
	}
	return breaks, line+tail <= limit
}

// applyFuncSignatureLayout stamps a shared parameter/result layout. A nil
// field represents a fixed unit, such as the first result of a no-param func.
func applyFuncSignatureLayout(ft *dst.FuncType, fields []*dst.Field,
	breaks []int) bool {

	for _, i := range breaks {
		if fields[i] == nil {
			return false
		}
	}
	clearFieldDecorations(ft.Params.List)
	if ft.Results != nil {
		clearFieldDecorations(ft.Results.List)
	}
	for _, i := range breaks {
		fields[i].Decs.Before = dst.NewLine
	}
	return true
}

// funcLiteralWrap applies R3 to closures after R4 has chosen their argument
// layout. A reflowed closure is repacked even without --optimize; R2 then
// removes a stale body blank when the signature collapses to one line.
type funcLiteralWrap struct{}

func (funcLiteralWrap) Name() string { return "R3" }

func (funcLiteralWrap) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.FuncDefWrapOn() {
		return nil
	}
	limit, tab := ctx.Config.LineLength, ctx.Config.TabWidth
	if tab <= 0 {
		tab = 8
	}
	parents := buildDstParents(ctx.File)
	dst.Inspect(ctx.File, func(n dst.Node) bool {
		if ctx.SkipFormatting(n) {
			return false
		}
		lit, ok := n.(*dst.FuncLit)
		if !ok || lit.Type == nil || lit.Type.Params == nil {
			return true
		}
		al, ok := ctx.Decorator.Ast.Nodes[lit].(*ast.FuncLit)
		if !ok || al.Body == nil {
			return true
		}
		if !ctx.Config.Optimize && !ctx.ReflowedArgs[lit] &&
			sigLinesFit(ctx, al.Type, al.Body, limit, tab) {

			return true
		}
		fields, widths, seed, tail, ok := funcSignatureParts(
			ctx, al.Type, lit.Type, al.Body.Lbrace, tab,
		)
		if !ok {
			return true
		}
		fset, lines := ctx.FileSet, ctx.SourceLines
		firstCol := visualCol(fset, lines, al.Pos(), tab)
		indent := lineIndentAt(fset, lines, al.Pos(), tab)
		if ctx.ReflowedArgs[lit] {
			if call, ok := parents[lit].(*dst.CallExpr); ok {
				firstCol, indent = callArgPosition(
					ctx, call, lit, parents, tab,
				)
			}
		}
		breaks, ok := packFuncDef(
			widths, seed, tail, firstCol, indent+tab, limit,
		)
		if ok {
			applyFuncSignatureLayout(lit.Type, fields, breaks)
		}
		return true
	})
	return nil
}
