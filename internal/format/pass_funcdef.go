package format

import (
	"go/ast"
	"go/token"

	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// funcDefWrap implements R3: function signatures are laid out by greedy
// packing. Single-line if it fits; otherwise params packed across lines, with
// the closing ")" attached to the last param (the doc forbids dangling
// close-parens on function definitions).
//
// Runs on every FuncDecl — including ones whose source layout was "acceptable
// but not packed". Per the user's rule, we re-pack to fill each line up to the
// limit (unwrap unnecessarily-spread params).
//
// Single-param signatures are left alone in v1: there's nothing to pack
// between, and breaking before the lone param would put a dangling `(` at end
// of the func line, which the doc forbids in most contexts.
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
		fd, ok := n.(*dst.FuncDecl)
		if !ok {
			return true
		}
		if fd.Type == nil || fd.Type.Params == nil {
			return true
		}
		if len(fd.Type.Params.List) < 2 {
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
		// resolve an over-limit line. If every line the signature occupies
		// already fits, the author's layout is valid — leave it. Repacking
		// or collapsing a fitting signature is a SOFT, --optimize-only
		// change. (R2's body-blank still works: MultilineSigs is seeded
		// from the source state in analyse().)
		if !ctx.Config.Optimize && sigLinesFit(ctx, astFD, limit, tab) {
			return true
		}

		breaks, multiLine := decideFuncDefLayout(
			ctx, astFD, fd, limit, tab,
		)
		applyFuncDefLayout(fd, breaks, multiLine, ctx)
		return true
	})
	return nil
}

// sigLinesFit reports whether every source line the function signature occupies
// — from the `func` keyword through the opening body brace — is within the
// limit. When true, the signature's current layout is structurally valid and
// must be left untouched in the default (non-optimize) mode.
func sigLinesFit(ctx *Context, fd *ast.FuncDecl, limit, tab int) bool {
	fset := ctx.FileSet
	start := fset.Position(fd.Type.Func).Line
	end := fset.Position(fd.Body.Lbrace).Line
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

func decideFuncDefLayout(ctx *Context, astFD *ast.FuncDecl, fd *dst.FuncDecl,
	limit, tab int) (breaks []int, multiLine bool) {

	fset := ctx.FileSet
	lines := ctx.SourceLines

	// seedWidth: visual width of "func <recv> name(" — everything up to
	// and including the params' opening paren. Measured on the source line
	// containing the func keyword.
	seedW := sourceWidth(
		fset, lines, astFD.Type.Func, astFD.Type.Params.Opening+1, tab,
	)

	// trailingW: visual width of ") <results> {" on the source line
	// containing the params' closing paren. The source's `)` and `{` are on
	// the same line for any reasonably-formatted Go input.
	trailingW := sourceWidth(
		fset, lines, astFD.Type.Params.Closing, astFD.Body.Lbrace+1,
		tab,
	)
	if trailingW == wideForcedBreak {
		// Fallback: estimate manually. ") " + " {" plus rough results.
		trailingW = estimateTrailing(astFD)
	}

	// funcCol: visual indent of `func` keyword (the line's leading
	// whitespace consumed by the time we hit it).
	funcCol := visualCol(fset, lines, astFD.Type.Func, tab)

	widths := fieldWidths(fset, lines, astFD.Type.Params.List, tab)
	sumW := sum(widths)
	seps := (len(widths) - 1) * 2 // ", " between params

	singleLine := funcCol + seedW + sumW + seps + trailingW
	if singleLine <= limit {
		return nil, false
	}

	firstBudget := limit - (funcCol + seedW)
	contIndent := lineIndentAt(ctx.FileSet, lines, astFD.Type.Func, tab) +
		tab
	contBudget := limit - contIndent
	breaks = packLayout(widths, firstBudget, contBudget, trailingW)
	return breaks, true
}

// estimateTrailing builds the trailing text length manually when the source
// positions span lines and we can't measure directly.
func estimateTrailing(fd *ast.FuncDecl) int {
	w := 1 // ")"
	if fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
		w += 1 // " "
		results := fd.Type.Results.List
		useParens := len(results) > 1 ||
			(len(results) == 1 && len(results[0].Names) > 0)
		if useParens {
			w += 1 // "("
		}
		for i, r := range results {
			if i > 0 {
				w += 2 // ", "
			}
			for j, n := range r.Names {
				if j > 0 {
					w += 2 // ", "
				}
				w += len(n.Name)
			}
			if len(r.Names) > 0 {
				w += 1 // " "
			}
			if r.Type != nil {
				w += astExprLen(r.Type)
			}
		}
		if useParens {
			w += 1 // ")"
		}
	}
	if fd.Body != nil {
		w += 2 // " {"
	}
	return w
}

// astExprLen returns a rough rendered width for a type expression. Used only by
// the trailing-estimate fallback, so cheap > correct.
func astExprLen(e ast.Expr) int {
	switch x := e.(type) {
	case *ast.Ident:
		return len(x.Name)

	case *ast.StarExpr:
		return 1 + astExprLen(x.X)

	case *ast.SelectorExpr:
		return astExprLen(x.X) + 1 + len(x.Sel.Name)

	case *ast.ArrayType:
		return 2 + astExprLen(x.Elt)

	case *ast.Ellipsis:
		return 3 + astExprLen(x.Elt)
	}
	return 16 // pessimistic fallback
}

func applyFuncDefLayout(fd *dst.FuncDecl, breaks []int, multiLine bool,
	ctx *Context) {

	params := fd.Type.Params.List
	clearFieldDecorations(params)

	if !multiLine {
		// Single-line target — fix-only-but-canonicalize-multi-line
		// for R3: ensure no entry in MultilineSigs so R2 doesn't add a
		// body blank line that no longer applies.
		delete(ctx.MultilineSigs, fd)
		return
	}

	for _, i := range breaks {
		if i >= 0 && i < len(params) {
			params[i].Decs.Before = dst.NewLine
		}
	}
	ctx.MultilineSigs[fd] = true
}

// token used to convince the compiler we still import token even when no other
// reference appears (gofmt would otherwise remove the import).
var _ token.Pos
