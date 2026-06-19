package format

import (
	"bytes"
	"go/ast"
	"go/token"
	"strings"

	"github.com/dave/dst"
)

// splitLines returns the input split on '\n' without the line terminators. An
// empty trailing segment (from a terminating newline) is dropped.
func splitLines(src []byte) [][]byte {
	lines := bytes.Split(src, []byte("\n"))
	if n := len(lines); n > 0 && len(lines[n-1]) == 0 {
		lines = lines[:n-1]
	}
	return lines
}

// visualWidth measures a line's rendered width, expanding tabs to the next
// multiple of tab. Bytes outside ASCII count as one column each; that's an
// approximation but matches gofmt's column accounting for the source-line
// widths we care about (decision-making for wrap rules).
func visualWidth(line []byte, tab int) int {
	col := 0
	for _, b := range line {
		if b == '\t' {
			col += tab - (col % tab)
			continue
		}
		col++
	}
	return col
}

// sourceLineWidth returns the visual width of the source line containing pos.
func sourceLineWidth(fset *token.FileSet, lines [][]byte, pos token.Pos,
	tab int) int {

	p := fset.Position(pos)
	if p.Line <= 0 || p.Line > len(lines) {
		return 0
	}
	return visualWidth(lines[p.Line-1], tab)
}

// visualCol returns the 0-based visual column where the character at pos
// begins, accounting for tab expansion. Used by layout passes that need to know
// "where on the line does this token start" — typically to compute first-line
// budgets for argument packing.
func visualCol(fset *token.FileSet, lines [][]byte, pos token.Pos,
	tab int) int {

	p := fset.Position(pos)
	if p.Line <= 0 || p.Line > len(lines) {
		return 0
	}
	line := lines[p.Line-1]
	col := p.Column - 1
	if col > len(line) {
		col = len(line)
	}
	return visualWidth(line[:col], tab)
}

// lineIndentAt returns the visual indent of the source line containing pos —
// the leading-whitespace width with tabs expanded. Used by wrap passes to
// compute continuation-line indent: gofmt continues a wrapped expression at the
// statement's indent + one tab, NOT the call's column
// + one tab. For a deeply-nested call inside a statement, the latter
// would shrink the budget far below what gofmt actually emits.
func lineIndentAt(fset *token.FileSet, lines [][]byte, pos token.Pos,
	tab int) int {

	p := fset.Position(pos)
	if p.Line <= 0 || p.Line > len(lines) {
		return 0
	}
	src := lines[p.Line-1]
	end := 0
	for end < len(src) && (src[end] == '\t' || src[end] == ' ') {
		end++
	}
	return visualWidth(src[:end], tab)
}

// isSingleLine reports whether two positions are on the same source line.
func isSingleLine(fset *token.FileSet, a, b token.Pos) bool {
	return fset.Position(a).Line == fset.Position(b).Line
}

// calleeName returns the dotted name of a CallExpr's callee, e.g. "fmt.Errorf"
// or "log.Debugf" or "someFunc". Returns "" for callees that don't resolve to a
// simple identifier or selector (e.g. a method call on an arbitrary
// expression). Used by R5 to consult the formatting-funcs allowlist
// syntactically.
func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name

	case *ast.SelectorExpr:
		switch x := fn.X.(type) {
		case *ast.Ident:
			return x.Name + "." + fn.Sel.Name

		case *ast.SelectorExpr:
			// pkg.Type.Method — return last two segments only;
			// callers match against suffixes.
			return x.Sel.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	}
	return ""
}

// markInnerCallsHandled walks call.Args (the dst tree, since we may have
// already inserted new BinaryExpr nodes that don't exist in ast) and records
// every CallExpr descendant in the OuterHandled set. R4 then skips them —
// their effective column is no longer their source column, so source-column
// measurement would over-wrap them.
//
// FuncLit bodies are not descended into: a closure body has its own independent
// indentation scope, and calls inside it sit on their own source lines that
// R4's source-positional measurements handle correctly. Marking them as handled
// would prevent legitimate wraps — for instance, a long
// chainhash.NewHashFromStr(...) inside a t.Run("...", func() { ... }) block.
func markInnerCallsHandled(handled map[*dst.CallExpr]bool, call *dst.CallExpr) {
	for _, arg := range call.Args {
		dst.Inspect(arg, func(n dst.Node) bool {
			if n == nil {
				return false
			}
			if _, isFunc := n.(*dst.FuncLit); isFunc {
				return false
			}
			if inner, ok := n.(*dst.CallExpr); ok && inner != call {
				handled[inner] = true
			}
			return true
		})
	}
}

// markInnerCallsHandledDeep is markInnerCallsHandled without the FuncLit
// boundary skip. Used by R6's symmetric layout when the multi-line container is
// itself a FuncLit: the body lives one tab deeper than the call, and source
// positions for inner statements may be at a deeper indent than the final
// render. Marking everything keeps R4 from re-wrapping inner calls that already
// fit at the post-shift depth.
func markInnerCallsHandledDeep(handled map[*dst.CallExpr]bool,
	call *dst.CallExpr) {

	for _, arg := range call.Args {
		dst.Inspect(arg, func(n dst.Node) bool {
			if n == nil {
				return false
			}
			if inner, ok := n.(*dst.CallExpr); ok && inner != call {
				handled[inner] = true
			}
			return true
		})
	}
}

// inStringSet reports whether name matches any entry in set. Matching compares
// the trailing segment after the last `.` on each side — so an allowlist
// entry "log.Debugf" matches `log.Debugf`, `srvrLog.Debugf`, or a bare `Debugf`
// identifier. This is the right semantics for the formatting-funcs allowlist
// and the deny list, where the convention is "any method named Debugf is a
// debug-format function regardless of receiver name". Exact-only matching is
// the wrong default — codebases alias loggers under many names.
func inStringSet(name string, set []string) bool {
	if name == "" {
		return false
	}
	nameSuffix := trailingSegment(name)
	for _, s := range set {
		if s == name || trailingSegment(s) == nameSuffix {
			return true
		}
	}
	return false
}

func trailingSegment(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

// inStringSetExact is the strict variant: only full-string match counts. Used
// for the deny list, where suffix matching would over-reject (e.g. "t.Errorf"
// in deny would otherwise reject every `*.Errorf` call, including fmt.Errorf
// which is in the allow list).
func inStringSetExact(name string, set []string) bool {
	if name == "" {
		return false
	}
	for _, s := range set {
		if s == name {
			return true
		}
	}
	return false
}
