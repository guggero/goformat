package format

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// funcCallWrap implements R4: function calls laid out by greedy packing.
// Single-line if it fits; otherwise args wrap onto continuation lines (one tab
// deeper than the call), filled left-to-right up to the limit, with the closing
// ")" on its own line at the call's indent (and a trailing comma after the last
// arg, as gofmt requires).
//
// Runs on every CallExpr. Two opt-outs preserve existing layout:
//   - R5: callee is in the formatting-funcs allowlist (and not in deny).
//   - R8: callee is a structured-log method (handled by structuredLogWrap).
//
// The single-arg guard remains: calls like make(map[K]V) wrap badly when their
// lone argument is a type expression, so we skip them and let R10 surface any
// overrun.
type funcCallWrap struct{}

func (funcCallWrap) Name() string { return "R4" }

func (funcCallWrap) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.FuncCallWrapOn() {
		return nil
	}
	limit := ctx.Config.LineLength
	tab := ctx.Config.TabWidth
	if tab <= 0 {
		tab = 8
	}
	fmtFns := ctx.Config.FormattingFuncs
	denyFns := ctx.Config.FormattingFuncsDeny
	r5On := ctx.Config.Rules.FormattingFnCompactOn()
	r8On := ctx.Config.Rules.StructuredLogWrapOn()
	logMethods := ctx.Config.StructuredLogMethods
	parents := buildDstParents(ctx.File)

	dst.Inspect(ctx.File, func(n dst.Node) bool {
		call, ok := n.(*dst.CallExpr)
		if !ok {
			return true
		}
		if ctx.OuterHandled[call] {
			return true
		}
		if len(call.Args) == 0 {
			return true
		}

		// Skip single-arg calls only when wrapping would visibly hurt:
		//   * `make(...)` and `new(...)` builtins — the lone arg is a
		//     type expression and wrapping puts the type on its own
		//     line, which reads worse than the source.
		//   * any other single-arg bare-Ident call whose lone arg is
		//     itself a type expression (map/array/chan/struct/...).
		// Single-arg calls whose arg is a VALUE expression (call,
		// composite, identifier, ...) ARE wrapped when overlong —
		// e.g. int32(binary.LittleEndian.Uint32(value)) at 88 cols
		// usefully wraps the inner call onto its own continuation line.
		if len(call.Args) == 1 {
			if ident, isIdent := call.Fun.(*dst.Ident); isIdent {
				if ident.Name == "make" || ident.Name == "new" {
					return true
				}
				if isDstTypeExpr(call.Args[0]) {
					return true
				}
			}
		}
		astN, ok := ctx.Decorator.Ast.Nodes[call]
		if !ok {
			return true
		}
		astCall, ok := astN.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Skip calls whose callee spans multiple source lines. The
		// classic case is a method chain —
		// txscript.NewScriptBuilder().
		// AddOp(opCode).AddData(buf).Script() — where each
		// `.Method(...)` is its own call but Fun extends back to the
		// chain's start, and any source-positional measurement
		// (single-line projected width, etc.) gets the wrong answer.
		// Leaving them alone is strictly safer than over-wrapping.
		if ctx.FileSet.Position(astCall.Fun.Pos()).Line !=
			ctx.FileSet.Position(astCall.Fun.End()).Line {

			return true
		}

		// HARD-only by default: a call is reformatted solely to resolve an
		// over-limit line. If every line the call occupies already fits,
		// the author's layout is structurally valid, so leave it untouched.
		// Space-efficiency reflows (collapsing a multi-line call, repacking
		// a one-per-line layout, re-imposing symmetry on fitting code) are
		// SOFT — opt in with --optimize.
		if !ctx.Config.Optimize &&
			allCallLinesFit(ctx, astCall, limit, tab) {

			return true
		}

		// Layout-fragile contexts: when a call is already multi-line
		// in source AND every line fits, AND it sits inside an
		// alignment-sensitive or operator-broken parent, collapsing
		// it would oscillate. Two known patterns:
		//
		//   - inside a struct literal: collapsing to single-line lets
		//     gofmt insert alignment padding that pushes the line
		//     back over the limit on the next run.
		//   - inside an operator-split binary expression: R4's per-
		//     call projected width misses sibling content on adjacent
		//     lines, so collapsing creates an over-limit composite
		//     line that R4 then re-wraps on the next run.
		//
		// In other contexts (var = errors.New("..." + "..."), etc.)
		// R4's normal reflow is still allowed — R9 may join the
		// concat to a single literal and R4 needs to wrap the call.
		sourceMultiLine := ctx.FileSet.Position(astCall.Pos()).Line !=
			ctx.FileSet.Position(astCall.End()).Line
		if sourceMultiLine &&
			allCallLinesFit(ctx, astCall, limit, tab) &&
			insideLayoutFragileParent(parents, call) {

			return true
		}

		// R8 owns structured-log calls; skip them here.
		if r8On && isStructuredLogCall(astCall, logMethods) {
			return true
		}

		// R5: formatting funcs get string-splitting, not the R4 wrap.
		if r5On && isFormattingCall(ctx, call, fmtFns, denyFns) {
			if applyFormattingCallLayout(
				ctx, astCall, call, limit, tab,
			) {

				markInnerCallsHandled(ctx.OuterHandled, call)
			}
			return true
		}

		kind, breaks := decideCallLayout(ctx, astCall, call, limit, tab)
		switch kind {
		case layoutCollapse:
			applyCallLayout(call, nil, false)

		case layoutSymmetric:
			// Clear all NewLine markers on the outer args; the
			// multi-line container's internal decorations stay in
			// place, so the result is "args inline up to and
			// including the container's open token, container
			// internals on continuation lines, container close and
			// outer ')' on a shared closing line."
			clearArgDecorations(call.Args)
			// When the container is a FuncLit, the symmetric layout
			// shifts its body indent to (callIndent + tab), which
			// may be SHALLOWER than the source. R4 measures
			// inner-call wrap decisions from source positions, so
			// without intervention it would over-wrap inner calls
			// whose source line is over-limit but whose post-shift
			// rendered line fits. Mark every inner call —
			// including those nested inside FuncLit bodies — so
			// R4 leaves them alone; R10's post-render check still
			// flags any line that's still over after the shift.
			markInnerCallsHandledDeep(ctx.OuterHandled, call)

		case layoutPack:
			applyCallLayout(call, breaks, true)
			markInnerCallsHandled(ctx.OuterHandled, call)
		}
		return true
	})
	return nil
}

// layoutKind selects between three resolutions for an overlong call.
type layoutKind int

const (
	layoutCollapse  layoutKind = iota // single-line fits the limit
	layoutSymmetric                   // last arg is a multi-line container; outer args inline
	layoutPack                        // every arg on its own continuation line, ')' on own line
)

func decideCallLayout(ctx *Context, astCall *ast.CallExpr, call *dst.CallExpr,
	limit, tab int) (layoutKind, []int) {

	fset := ctx.FileSet
	lines := ctx.SourceLines

	callCol := visualCol(fset, lines, astCall.Pos(), tab)
	calleeW := sourceWidth(
		fset, lines, astCall.Fun.Pos(), astCall.Fun.End(), tab,
	)

	widths := argWidths(fset, lines, astCall.Args, tab)
	n := len(widths)
	sumW := sum(widths)
	seps := (n - 1) * 2

	// 1. Try single-line. Multi-line args contribute wideForcedBreak to
	//    sumW, so this only fires when every arg is single-line. The
	//    width we check is the PROJECTED line width if the call were
	//    collapsed — pre-call text + single-line call width + post-call
	//    text. For a call already on one source line that equals the
	//    source line width; for a multi-line source call it's the line
	//    we'd produce by collapsing, which may differ (e.g. an if
	//    header's "; err != nil {" tail attaches after the collapse).
	callW := calleeW + 1 + sumW + seps + 1
	postW := postCallLineWidth(fset, lines, astCall.End(), tab)
	projected := callCol + callW + postW
	if projected <= limit {
		return layoutCollapse, nil
	}

	// 2. Try indentation-symmetric layout. Any arg may be the multi-line
	//    container (call / composite / closure / &Composite). The
	//    synthesised first line is "outer args up to and including the
	//    container's open token"; the synthesised closing line is
	//    "container's close + remaining args + outer ')'". Both must
	//    fit. With container at the LAST position, this matches the
	//    classic doc example f(a, &T{...}); with container in the
	//    MIDDLE (e.g. dstutil.Apply(file, func(...) { ... }, nil)),
	//    args after the container ride the closing line.
	containerIdx := -1
	for i := 0; i < n; i++ {
		if isMultiLineContainer(call.Args[i]) {
			containerIdx = i
			break
		}
	}
	if containerIdx >= 0 {
		openW := openTokenWidth(
			fset, lines, astCall.Args[containerIdx], tab,
		)
		if openW > 0 {
			preLine := callCol + calleeW + 1
			for i := 0; i < containerIdx; i++ {
				if i > 0 {
					preLine += 2
				}
				preLine += widths[i]
			}
			if containerIdx > 0 {
				preLine += 2
			}
			preLine += openW

			postIndent := lineIndentAt(
				fset, lines, astCall.Pos(), tab,
			)
			postLine := postIndent + 1 // container close
			for i := containerIdx + 1; i < n; i++ {
				postLine += 2 // ", " before this arg
				postLine += widths[i]
			}
			postLine += 1 // outer ")"

			if preLine <= limit && postLine <= limit {
				return layoutSymmetric, nil
			}
		}
	}

	// 3. Fall back to packed verbose layout. If the last arg is itself
	//    a multi-line container, swap its (effectively-infinite) width
	//    for its open-token width so the packer can correctly evaluate
	//    "do the outer args + container-open fit on a continuation
	//    line?". When they do, packLayout returns no breaks and the
	//    apply step produces the wrapped-symmetric form — outer args
	//    inline on a continuation, container opens at end of that
	//    line, internals follow on deeper continuations.
	if n >= 1 {
		if isMultiLineContainer(call.Args[n-1]) {
			if w := openTokenWidth(
				fset, lines, astCall.Args[n-1], tab,
			); w > 0 {

				widths[n-1] = w
			}
		}
	}
	contIndent := lineIndentAt(fset, lines, astCall.Pos(), tab) + tab
	contBudget := limit - contIndent
	breaks := packLayout(widths, contBudget, contBudget, 1)
	return layoutPack, breaks
}

// openTokenWidth returns the visual width of an expression's "opening token"
// — the prefix up to and including the first `{` or `(` that introduces a
// multi-line body. For &Foo{...} the open token is "&Foo{"; for f(x, y) it's
// "f("; for `func(a int) bool {` it's the whole header. Returns 0 if the
// expression has no recognisable open token (i.e. isMultiLineContainer would
// return false).
func openTokenWidth(fset *token.FileSet, lines [][]byte, expr ast.Expr,
	tab int) int {

	var lbrace token.Pos
	switch x := expr.(type) {
	case *ast.CompositeLit:
		lbrace = x.Lbrace

	case *ast.CallExpr:
		lbrace = x.Lparen

	case *ast.FuncLit:
		if x.Body != nil {
			lbrace = x.Body.Lbrace
		}

	case *ast.UnaryExpr:
		return openTokenWidth(fset, lines, x.X, tab)
	}
	if !lbrace.IsValid() {
		return 0
	}

	// Width of "expr-up-to-lbrace" (exclusive) plus 1 for the `{` or `(`.
	w := sourceWidth(fset, lines, expr.Pos(), lbrace, tab)
	if w >= wideForcedBreak {
		return 0
	}
	return w + 1
}

func applyCallLayout(call *dst.CallExpr, breaks []int, multiLine bool) {
	clearArgDecorations(call.Args)
	if !multiLine {
		return
	}

	// All args go on continuation lines: the first arg always starts a new
	// line (after the open paren), and each break stamps another.
	call.Args[0].Decorations().Before = dst.NewLine
	for _, i := range breaks {
		if i >= 0 && i < len(call.Args) {
			call.Args[i].Decorations().Before = dst.NewLine
		}
	}

	// Close paren on its own line — flag via After on the last arg, which
	// gofmt translates to "trailing comma + ) on next line".
	call.Args[len(call.Args)-1].Decorations().After = dst.NewLine
}

// applyFormattingCallLayout implements R5: split the format string with "+" at
// a sensible point so line 1 (`f(... "part1" +`) fits within the limit, place
// "part2" on a continuation line, and keep the remaining args inline after
// "part2". Returns true if it applied a transformation; false means we left the
// call alone (e.g. args[0] isn't a string literal we can split).
//
// Layout target — for an overlong fmt.Errorf:
//
//	err := fmt.Errorf("this is a long error message that we definitely "+
//		"want %d", count)
func applyFormattingCallLayout(ctx *Context, astCall *ast.CallExpr,
	call *dst.CallExpr, limit, tab int) bool {

	fset := ctx.FileSet
	lines := ctx.SourceLines

	// Bail out if the call already fits — fix-only.
	callCol := visualCol(fset, lines, astCall.Pos(), tab)
	calleeW := sourceWidth(
		fset, lines, astCall.Fun.Pos(), astCall.Fun.End(), tab,
	)
	widths := argWidths(fset, lines, astCall.Args, tab)

	// If the call is already multi-line in source and every line fits,
	// preserve the developer's choice. The doc accepts multiple valid
	// formatting-call layouts (arg-on-continuation, string-on-its-own-
	// line, compact-"+", …); re-flowing one fitting form to another just
	// churns diffs.
	sourceMultiLine := fset.Position(astCall.Pos()).Line !=
		fset.Position(astCall.End()).Line
	if sourceMultiLine && allCallLinesFit(ctx, astCall, limit, tab) {
		return false
	}

	singleLine := callCol + calleeW + 1 + sum(widths) + (len(widths)-1)*2 +
		1
	if singleLine <= limit {
		clearArgDecorations(call.Args)
		return false
	}

	// args[0] must be a splittable interpreted string literal.
	lit, ok := call.Args[0].(*dst.BasicLit)
	if !ok {
		return false
	}
	if lit.Kind != token.STRING {
		return false
	}
	v := lit.Value
	if len(v) < 4 || v[0] != '"' || v[len(v)-1] != '"' {
		return false
	}
	body := v[1 : len(v)-1]

	// Only bail on backslashes when there's nowhere safe to split.
	// Splitting at a space is always safe: an escape sequence (`\n`, `\"`,
	// …) never spans a space. Only the no-space fallback could land
	// mid-escape, and we skip that case here.
	if strings.ContainsRune(body, '\\') &&
		!strings.ContainsRune(body, ' ') {

		return false
	}

	// Budget for part1 body content. Line 1 will render as:
	//   <pre-text>"<body>" +
	// where pre-text extends up to the open quote (callCol + calleeW + 1
	// for the `(`). The +4 reserves room for "<body>" + (the two quotes, a
	// space, and the +). Slightly conservative — gofmt sometimes drops
	// the space inside a call expression — but never overshoots the
	// limit.
	pre := callCol + calleeW + 1
	budget := limit - pre - 4
	if budget < 1 || budget >= len(body) {
		return false
	}

	splitAt := findStringSplit(body, budget)
	if splitAt <= 0 || splitAt >= len(body) {
		return false
	}

	left := &dst.BasicLit{
		Kind:  token.STRING,
		Value: `"` + body[:splitAt] + `"`,
	}
	right := &dst.BasicLit{
		Kind:  token.STRING,
		Value: `"` + body[splitAt:] + `"`,
	}
	right.Decs.Before = dst.NewLine

	call.Args[0] = &dst.BinaryExpr{X: left, Op: token.ADD, Y: right}

	// Subsequent args stay on the line they end up on (after part2). Clear
	// any stale NewLines so they don't accidentally inherit one.
	for i := 1; i < len(call.Args); i++ {
		decs := call.Args[i].Decorations()
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
	return true
}

// findStringSplit returns the byte index at which to split a string literal's
// body. Preference order:
//  1. Rightmost space at or before budget (split keeps the space with
//     part1, so the rendered result has a visible word break).
//  2. Exact budget position if no space is found (hex / identifier-
//     style strings — better to break mechanically than to leave the
//     line overlong).
//
// Returns 0 if budget is impractical (too small for any split).
func findStringSplit(body string, budget int) int {
	if budget < 1 {
		return 0
	}
	if budget >= len(body) {
		return 0
	}
	for i := budget; i > 0; i-- {
		if body[i] == ' ' {
			return i + 1 // keep the space with part1
		}
	}
	return budget
}

// isDstTypeExpr reports whether expr is unambiguously a type expression (the
// kind of thing that appears as the first arg to `make` or `new`, or as the
// type in a conversion). For Ident, SelectorExpr, and StarExpr we can't tell
// syntactically — those could be either value or type — so we
// conservatively return false and let R4 wrap.
func isDstTypeExpr(e dst.Expr) bool {
	switch e.(type) {
	case *dst.MapType, *dst.ArrayType, *dst.ChanType,
		*dst.StructType, *dst.InterfaceType, *dst.FuncType:
		return true
	}
	return false
}

// allCallLinesFit reports whether every source line in a call's span is within
// the configured width limit. Used by R5 to leave a valid multi-line formatting
// call alone rather than rewriting it into the compact "+" form when the
// developer's split already fits.
func allCallLinesFit(ctx *Context, call *ast.CallExpr, limit, tab int) bool {
	fset := ctx.FileSet
	startLine := fset.Position(call.Pos()).Line
	endLine := fset.Position(call.End()).Line
	for ln := startLine; ln <= endLine; ln++ {
		if ln <= 0 || ln > len(ctx.SourceLines) {
			return false
		}
		if visualWidth(ctx.SourceLines[ln-1], tab) > limit {
			return false
		}
	}
	return true
}

// isMultiLineAndAllLinesFit reports whether the expression spans multiple
// source lines and every line it touches is within the limit. The
// "leave-it-alone" signal for R4 and R9 — once a multi-line layout fits,
// re-flowing it just churns diffs and risks instability when an enclosing
// struct literal's gofmt alignment widens the line on collapse.
func isMultiLineAndAllLinesFit(ctx *Context, e ast.Expr, limit, tab int) bool {
	startLine := ctx.FileSet.Position(e.Pos()).Line
	endLine := ctx.FileSet.Position(e.End()).Line
	if startLine == endLine {
		return false
	}
	for ln := startLine; ln <= endLine; ln++ {
		if ln <= 0 || ln > len(ctx.SourceLines) {
			return false
		}
		if visualWidth(ctx.SourceLines[ln-1], tab) > limit {
			return false
		}
	}
	return true
}

// insideLayoutFragileParent reports whether the node sits inside a parent
// whose final rendering depends on whether children are single- or
// multi-line. Two parent shapes qualify:
//
//   - KeyValueExpr inside a CompositeLit (struct or map literal): gofmt
//     auto-aligns colons across consecutive single-line keyed fields, so
//     a value that collapses to single-line might be padded and pushed
//     over the limit by the alignment, then re-wrapped on the next run.
//   - A multi-line BinaryExpr: when an operator break already exists,
//     collapsing one operand changes the joined line width on a
//     different source line, which R4's per-call projection doesn't see.
func insideLayoutFragileParent(parents map[dst.Node]dst.Node, n dst.Node) bool {
	cur := n
	for {
		p, ok := parents[cur]
		if !ok || p == nil {
			return false
		}
		switch parent := p.(type) {
		case *dst.KeyValueExpr:
			// A keyed entry inside a CompositeLit: subject to
			// gofmt's alignment padding.
			if gp, ok := parents[parent]; ok {
				if _, isComp := gp.(*dst.CompositeLit); isComp {
					return true
				}
			}

		case *dst.BinaryExpr:
			if subtreeHasNewLineDec(parent) {
				return true
			}
		}
		cur = p
	}
}

// isMultiLineContainer reports whether expr is a "container" that's laid out
// across multiple lines in the FINAL output. dst-based check: we look for a
// NewLine decoration on the container's first inner member (first call arg,
// first composite-lit elt, first body statement). This captures both
// source-multi-line containers and those R7 has just reflowed — without it,
// R4 wouldn't recognise an R7-reflowed struct as eligible for the
// inline-symmetric form.
func isMultiLineContainer(expr dst.Expr) bool {
	switch x := expr.(type) {
	case *dst.CompositeLit:
		if len(x.Elts) == 0 {
			return false
		}
		return hasNewLineBefore(x.Elts[0])

	case *dst.FuncLit:
		if x.Body == nil || len(x.Body.List) == 0 {
			return false
		}
		return hasNewLineBefore(x.Body.List[0])

	case *dst.CallExpr:
		if len(x.Args) == 0 {
			return false
		}
		return hasNewLineBefore(x.Args[0])

	case *dst.UnaryExpr:
		return isMultiLineContainer(x.X)
	}
	return false
}

func hasNewLineBefore(n dst.Node) bool {
	decs := n.Decorations()
	if decs == nil {
		return false
	}
	return decs.Before == dst.NewLine || decs.Before == dst.EmptyLine
}

// isFormattingCall reports whether the call's syntactic callee name is in the
// formatting-funcs allowlist (with suffix matching) and not in the deny list
// (exact only — see inStringSetExact comment).
func isFormattingCall(ctx *Context, call *dst.CallExpr,
	allow, deny []string) bool {

	astN, ok := ctx.Decorator.Ast.Nodes[call]
	if !ok {
		return false
	}
	astCall, ok := astN.(*ast.CallExpr)
	if !ok {
		return false
	}
	name := calleeName(astCall)
	if inStringSetExact(name, deny) {
		return false
	}
	return inStringSet(name, allow)
}
