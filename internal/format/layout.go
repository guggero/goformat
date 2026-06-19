package format

import (
	"go/ast"
	"go/token"

	"github.com/dave/dst"
)

// layout.go owns the "pack args greedily, fill each line up to the limit" logic
// used by R3 and R4. The two rules share width measurement and the greedy
// packer; they differ in where args attach (R3 packs from the func line; R4
// puts all args on continuation lines with close paren on its own line) and
// what trails the last param/arg.
//
// Width measurement is source-based: each token's width comes from its span in
// the original source bytes, tab-expanded to the configured width. This
// requires the source to be at least roughly gofmt-clean (consistent
// whitespace) but avoids the dst.Restorer machinery, which only prints whole
// files.

const wideForcedBreak = 1 << 16

// sourceWidth returns the visual width of the source text from start
// (inclusive) to end (exclusive), tab-expanded. Returns wideForcedBreak when
// the span crosses a line boundary — multi-line expressions can't be inlined
// into a pack, so the caller forces them onto their own line.
func sourceWidth(fset *token.FileSet, lines [][]byte, start, end token.Pos,
	tab int) int {

	startPos := fset.Position(start)
	endPos := fset.Position(end)
	if startPos.Line != endPos.Line {
		return wideForcedBreak
	}
	if startPos.Line <= 0 || startPos.Line > len(lines) {
		return 0
	}
	line := lines[startPos.Line-1]
	startCol := clamp(startPos.Column-1, 0, len(line))
	endCol := clamp(endPos.Column-1, 0, len(line))
	return visualWidth(line[:endCol], tab) -
		visualWidth(line[:startCol], tab)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// packLayout decides where to insert line breaks among a sequence of argument
// widths. The first arg is implicitly on the first line. A break at index i
// means args[i] starts a new line.
//
// firstBudget is the available width on the first line BEFORE the first arg
// starts (i.e. limit - first_arg_start_col). contBudget is the same for
// continuation lines. trailingLast is the width of whatever follows the last
// arg on its closing line — for R3 that's the rendered ") (returns) {" tail,
// for R4 with close on its own line it's just the trailing comma.
//
// The trailing constraint applies only to the LAST line. Middle lines just need
// room for their ", " separator. Treating every line as last-line would break
// the pack earlier than necessary and produce suboptimally-spread output (see
// R3 multi-param tests).
func packLayout(widths []int, firstBudget, contBudget, trailingLast int) []int {
	if len(widths) == 0 {
		return nil
	}
	var breaks []int
	const sepWidth = 2 // ", "

	lineWidth := widths[0]
	onFirstLine := true

	for i := 1; i < len(widths); i++ {
		isLast := i == len(widths)-1
		lineCap := contBudget
		if onFirstLine {
			lineCap = firstBudget
		}
		trail := 1 // ","
		if isLast {
			trail = trailingLast
		}
		budget := lineCap - trail
		prospective := lineWidth + sepWidth + widths[i]
		if prospective > budget {
			breaks = append(breaks, i)
			lineWidth = widths[i]
			onFirstLine = false
		} else {
			lineWidth = prospective
		}
	}
	return breaks
}

// clearArgDecorations resets Before/After NewLine markers on every arg. Layout
// passes call this before stamping in the new layout — without it, stale
// NewLine decorations from the input would survive and produce a hybrid (old +
// new) output.
func clearArgDecorations(args []dst.Expr) {
	for _, a := range args {
		decs := a.Decorations()
		if decs == nil {
			continue
		}
		if decs.Before == dst.NewLine || decs.Before == dst.EmptyLine {
			decs.Before = dst.None
		}
		if decs.After == dst.NewLine || decs.After == dst.EmptyLine {
			decs.After = dst.None
		}
	}
}

// clearFieldDecorations is the same for FieldList entries.
func clearFieldDecorations(fields []*dst.Field) {
	for _, f := range fields {
		if f == nil {
			continue
		}
		if f.Decs.Before == dst.NewLine ||
			f.Decs.Before == dst.EmptyLine {

			f.Decs.Before = dst.None
		}
		if f.Decs.After == dst.NewLine ||
			f.Decs.After == dst.EmptyLine {

			f.Decs.After = dst.None
		}
	}
}

// argWidths returns the rendered widths of every arg in a call. A multi-line
// arg (e.g. an inline composite literal that already spans lines) returns
// wideForcedBreak so it lands on its own packing line.
func argWidths(fset *token.FileSet, lines [][]byte, args []ast.Expr,
	tab int) []int {

	out := make([]int, len(args))
	for i, a := range args {
		out[i] = sourceWidth(fset, lines, a.Pos(), a.End(), tab)
	}
	return out
}

// fieldWidths is the same for params/results.
func fieldWidths(fset *token.FileSet, lines [][]byte, fields []*ast.Field,
	tab int) []int {

	out := make([]int, len(fields))
	for i, f := range fields {
		out[i] = sourceWidth(fset, lines, f.Pos(), f.End(), tab)
	}
	return out
}

func sum(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

// postCallLineWidth returns the visual width of source text from pos (typically
// a call's End()) to the end of pos's source line. Used by R4 to project the
// post-collapse line width — if a multi-line call is collapsed to
// single-line, the text that follows on the call's last source line (e.g. ";
// err != nil {") attaches behind the collapsed call, so we must include it when
// checking whether the collapsed form would fit.
func postCallLineWidth(fset *token.FileSet, lines [][]byte, pos token.Pos,
	tab int) int {

	p := fset.Position(pos)
	if p.Line <= 0 || p.Line > len(lines) {
		return 0
	}
	line := lines[p.Line-1]
	startCol := clamp(p.Column-1, 0, len(line))
	return visualWidth(line[startCol:], tab)
}
