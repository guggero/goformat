package format

import (
	"bytes"
	"go/ast"
	"go/token"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"

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

// Apply repairs call layout violations and optionally compacts fitting calls.
// Nested calls retain structural checks even when outer reflow has made their
// source columns unsuitable for width-only layout decisions.
func (funcCallWrap) Apply(ctx *Context) []diag.Diagnostic {
	r4On := ctx.Config.Rules.FuncCallWrapOn()
	r5On := ctx.Config.Rules.FormattingFnCompactOn()
	r6On := ctx.Config.Rules.IndentationSymmetryOn()
	if !r4On && !r5On && !r6On {
		return nil
	}
	limit := ctx.Config.LineLength
	tab := ctx.Config.TabWidth
	if tab <= 0 {
		tab = 8
	}
	fmtFns := ctx.Config.FormattingFuncs
	denyFns := ctx.Config.FormattingFuncsDeny
	r8On := ctx.Config.Rules.StructuredLogWrapOn()
	logMethods := ctx.Config.StructuredLogMethods
	parents := buildDstParents(ctx.File)

	dst.Inspect(ctx.File, func(n dst.Node) bool {
		if ctx.SkipFormatting(n) {
			return false
		}
		call, ok := n.(*dst.CallExpr)
		if !ok {
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

		// Width and structure are independent: a partially wrapped
		// ordinary call needs fixing even when every line fits. R5
		// and R8 own the compact layout exceptions for logging and
		// formatting calls.
		formattingCall := r5On &&
			isFormattingCall(ctx, call, fmtFns, denyFns)
		if !formattingCall && !r4On && !r6On {
			return true
		}
		structuredCall := r8On &&
			isStructuredLogCall(astCall, logMethods)
		needsLayoutFix := !formattingCall && !structuredCall &&
			!validCallLayout(ctx, astCall, tab)

		// Outer reflow invalidates source columns, so defer width-only
		// decisions for nested calls. Structural violations still
		// need repair: packing the outer arguments does not complete
		// a partially wrapped inner call's opening and closing lines.
		if ctx.OuterHandled[call] && !needsLayoutFix {
			return true
		}

		linesFit := allCallLinesFit(ctx, astCall, limit, tab)
		if !ctx.Config.Optimize && linesFit && !needsLayoutFix {

			return true
		}

		// Do no harm: if the callee plus its opening "(" already
		// exceeds the limit, no argument layout can bring that first
		// line under it (gofmt keeps "callee(" together — a long
		// selector chain like foo.bar.(*T).Method( can't be split).
		// Re-wrapping would only churn the closing layout without
		// fixing anything, so leave the call as-is and let R10 report
		// the unavoidable over-limit line.
		callCol := visualCol(
			ctx.FileSet, ctx.SourceLines, astCall.Pos(), tab,
		)
		calleeW := sourceWidth(
			ctx.FileSet, ctx.SourceLines,
			astCall.Fun.Pos(), astCall.Fun.End(), tab,
		)
		if calleeW < wideForcedBreak && callCol+calleeW+1 > limit {
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
		if sourceMultiLine && linesFit && !needsLayoutFix &&
			insideLayoutFragileParent(parents, call) {

			return true
		}

		// R8 owns structured-log calls; skip them here.
		if structuredCall {
			return true
		}

		// R5: formatting funcs get string-splitting, not the R4 wrap.
		if formattingCall {
			if applyFormattingCallLayout(
				ctx, astCall, call, limit, tab,
			) {

				markInnerCallsHandled(ctx.OuterHandled, call)
				return true
			}

			// Assertion operands or trailing values may leave no
			// room for a compact message. Fall back to R4 rather
			// than leave an avoidable overlong assertion line.
			_, assertion := requireMessageIndex(
				ctx.AstFile, astCall,
			)
			if !assertion || !r4On || linesFit {
				return true
			}
		}

		kind, breaks := decideCallLayout(ctx, astCall, call, limit, tab)
		if !r4On && kind != layoutSymmetric && kind != layoutPreserve {
			return true
		}
		if kind != layoutPreserve {
			for _, arg := range call.Args {
				ctx.ReflowedArgs[arg] = true
			}
		}
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
			clearCallArgLayout(call)
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

		case layoutPreserve:
			// Preserve an already compact layout, or a valid
			// fitting layout that symmetry cannot shorten. Keeping
			// ties as written avoids moving line breaks without
			// saving space. Mark inner calls so R4 doesn't descend
			// and undo inner layouts whose positions are
			// interpreted in the same way (each inner is its own
			// preserve/pack decision, no nested chaos).
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
	layoutPack                        // packed continuation lines, ')' on own line
	layoutPreserve                    // fitting layout needs no more compaction
)

// decideCallLayout selects a fitting call layout. Symmetry may repair invalid
// or overlong calls, but replaces a valid fitting layout only if it saves
// lines.
func decideCallLayout(ctx *Context, astCall *ast.CallExpr, call *dst.CallExpr,
	limit, tab int) (layoutKind, []int) {

	fset := ctx.FileSet
	lines := ctx.SourceLines

	// A chain of inline calls can share a deeper container's opening
	// line and close together, e.g. require.NoError(t, save(ctx, &T{...})).
	// Measuring only save's opening '(' misses this already compact form.
	if inlineSymmetricCall(ctx, astCall, tab, false) &&
		allCallLinesFit(ctx, astCall, limit, tab) {

		return layoutPreserve, nil
	}

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
	if ctx.Config.Rules.IndentationSymmetryOn() && containerIdx >= 0 {
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
				if !preferSymmetricLayout(
					ctx, astCall, call, containerIdx,
					postIndent+tab, limit, tab,
				) {

					return layoutPreserve, nil
				}

				// Re-pack the container's inner content. Its
				// layout may be stale (e.g. each arg on its
				// own line from a prior R4 pack-form layout)
				// where greedy packing now fits more args per
				// line. promoteToMultiline is the right hammer:
				// it stamps the same Before/After NewLine
				// pattern with greedily packed inner breaks,
				// matching what R4 / R7 would produce on a
				// fresh single-line input.
				promoteToMultiline(
					ctx, call.Args[containerIdx],
					astCall.Args[containerIdx],
					postIndent+tab, limit, tab,
				)
				return layoutSymmetric, nil
			}
		}
	}

	// 2b. No EXISTING multi-line container fit (or none was present).
	//     Try PROMOTING the LAST arg — if it's a single-line container
	//     (call / composite / &Composite) — to multi-line and then
	//     applying the symmetric layout. Symmetry can repair an overlong
	//     call or reduce the line count of an existing valid layout:
	//
	//         outerCall(args, innerCall(
	//             innerArg,
	//         ))
	//
	//     can replace an overlong call. An already fitting pack with
	//     the same number of lines is preserved:
	//
	//         outerCall(
	//             args, innerCall(innerArg),
	//         )
	//
	//     We restrict to the LAST arg deliberately: promoting an
	//     earlier arg would push every following arg onto the closing
	//     line, which often reads worse than the pack form (e.g.
	//     `po.addUnknown(byte(\n\tkeyCode,\n), keyData, value, ...)`).
	//     A last-arg promotion yields a clean "))" closing line.
	if ctx.Config.Rules.IndentationSymmetryOn() && n >= 1 {
		i := n - 1
		if cand, ok := promotableContainer(call.Args[i]); ok {
			astCand := astCall.Args[i]
			openW := openTokenWidth(
				fset, lines, astCand, tab,
			)
			if openW > 0 {
				preLine := callCol + calleeW + 1
				for j := 0; j < i; j++ {
					if j > 0 {
						preLine += 2
					}
					preLine += widths[j]
				}
				if i > 0 {
					preLine += 2
				}
				preLine += openW

				postIndent := lineIndentAt(
					fset, lines, astCall.Pos(), tab,
				)

				// Closing line: container close + outer ")". No
				// trailing args because this is the LAST arg.
				postLine := postIndent + 1 + 1

				if preLine <= limit && postLine <= limit &&
					promotedContainerFits(
						astCand, postIndent+tab,
						limit, fset, lines, tab,
					) {

					if !preferSymmetricLayout(
						ctx, astCall, call, i,
						postIndent+tab, limit, tab,
					) {

						return layoutPreserve, nil
					}

					promoteToMultiline(
						ctx, cand, astCand,
						postIndent+tab, limit, tab,
					)
					return layoutSymmetric, nil
				}
			}
		}
	}

	// Pack continuation lines around the opening and closing lines of
	// multi-line arguments. Their bodies do not consume horizontal space
	// on either line, so a closure in the middle can share both lines
	// with adjacent arguments.
	contIndent := lineIndentAt(fset, lines, astCall.Pos(), tab) + tab
	breaks := packCallArgs(ctx, astCall, call, contIndent, limit, tab)
	return layoutPack, breaks
}

// preferSymmetricLayout allows required repairs regardless of line count. For
// an already valid, fitting call, it previews symmetry and requires fewer
// rendered lines, preserving the author's layout on ties or measurement errors.
func preferSymmetricLayout(ctx *Context, ac *ast.CallExpr, call *dst.CallExpr,
	containerIdx, contIndent, limit, tab int) bool {

	// Line count is a preference, not a reason to retain broken layout.
	// Default mode already preserves fitting calls before reaching here;
	// this guard limits the extra rewrites enabled by optimization.
	if !validCallLayout(ctx, ac, tab) ||
		!allCallLinesFit(ctx, ac, limit, tab) {

		return true
	}

	// Preview on independent trees so rejecting a candidate cannot leave
	// inner argument decorations or reflow bookkeeping behind. Printing
	// both layouts also accounts for comments, blank lines, and containers
	// changed by earlier passes without trusting stale source line spans.
	original := dst.Clone(call).(*dst.CallExpr)
	candidate := dst.Clone(call).(*dst.CallExpr)
	preview := *ctx
	preview.ReflowedArgs = make(map[dst.Expr]bool)
	promoteToMultiline(
		&preview, candidate.Args[containerIdx], ac.Args[containerIdx],
		contIndent, limit, tab,
	)
	clearCallArgLayout(candidate)

	before, err := renderedCallLineCount(original)
	if err != nil {
		return false
	}
	after, err := renderedCallLineCount(candidate)
	return err == nil && after < before
}

// renderedCallLineCount prints a detached call in a fixed wrapper. The wrapper
// contributes the same lines to both candidates, while the standard printer
// resolves argument decorations and comments as it does in the final output.
func renderedCallLineCount(call *dst.CallExpr) (int, error) {
	file := &dst.File{
		Name: dst.NewIdent("p"),
		Decls: []dst.Decl{&dst.GenDecl{
			Tok: token.VAR,
			Specs: []dst.Spec{&dst.ValueSpec{
				Names:  []*dst.Ident{dst.NewIdent("_")},
				Values: []dst.Expr{call},
			}},
		}},
	}
	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, file); err != nil {
		return 0, err
	}
	return bytes.Count(buf.Bytes(), []byte{'\n'}), nil
}

// packCallArgs greedily packs wrapped arguments. A multi-line container has
// separate opening and closing widths: packing resumes after its closing
// token, at the same indentation as its opening line.
func packCallArgs(ctx *Context, ac *ast.CallExpr, call *dst.CallExpr,
	indent, limit, tab int) []int {

	fset, lines := ctx.FileSet, ctx.SourceLines
	budget := limit - indent
	line := 0
	var breaks []int
	for i, arg := range ac.Args {
		first := sourceWidth(fset, lines, arg.Pos(), arg.End(), tab)
		last := first
		multi := false
		if openTokenWidth(fset, lines, arg, tab) > 0 &&
			(first >= wideForcedBreak ||
				isMultiLineContainer(call.Args[i])) {

			multi = true
			if first >= wideForcedBreak {
				// Everything after the argument's start belongs
				// to its opening line. The closing line's
				// indentation will follow the newly packed
				// opening line.
				pos := fset.Position(arg.Pos())
				end := arg.Pos() + token.Pos(
					len(lines[pos.Line-1])-pos.Column+1,
				)
				first = sourceWidth(
					fset, lines, arg.Pos(), end, tab,
				)
				last = visualCol(
					fset, lines, arg.End(), tab,
				) - lineIndentAt(
					fset, lines, arg.End(), tab,
				)
			} else {
				// R7 may already have expanded a composite that
				// occupied one source line.
				first = openTokenWidth(fset, lines, arg, tab)
				last = 1
			}
		}
		if i == len(ac.Args)-1 && call.Ellipsis {
			last += 3
			if !multi {
				first += 3
			}
		}
		sep := 2 // ", " between arguments
		if i == 0 {
			sep = 0
		}
		trail := 1 // trailing comma on a packed argument line
		if multi {
			trail = 0 // the opening line continues inside the arg
		}
		if i > 0 && line+sep+first+trail > budget {
			breaks = append(breaks, i)
			line = first
		} else {
			line += sep + first
		}
		if multi {
			line = last
		}
	}
	return breaks
}

// validCallLayout accepts single-line calls, fully wrapped calls, and the
// inline symmetry exception. Width is checked separately. Arguments of a
// fully wrapped call must start after '(' and end before the closing line.
func validCallLayout(ctx *Context, call *ast.CallExpr, tab int) bool {
	fset, lines := ctx.FileSet, ctx.SourceLines
	start := fset.Position(call.Lparen).Line
	end := fset.Position(call.Rparen).Line
	if start == end {
		return true
	}
	if lineIndentAt(fset, lines, call.Pos(), tab) !=
		lineIndentAt(fset, lines, call.Rparen, tab) {

		return false
	}
	wrapped := true
	for _, arg := range call.Args {
		if fset.Position(arg.Pos()).Line <= start ||
			fset.Position(arg.End()).Line >= end {

			wrapped = false
			break
		}
	}
	return wrapped || inlineSymmetricCall(ctx, call, tab, true)
}

// inlineSymmetricCall follows a sequence of containers whose opening and
// closing lines have the call's indentation. A closing line may open the next
// container, as in append([]byte{...}, repeat([]byte{...}, count)...).
// Wrapped nested calls are structurally valid, but may still benefit from
// repacking during optimization. allowWrappedNested distinguishes those uses.
func inlineSymmetricCall(ctx *Context, call *ast.CallExpr, tab int,
	allowWrappedNested bool) bool {

	fset, lines := ctx.FileSet, ctx.SourceLines
	start := fset.Position(call.Lparen).Line
	end := fset.Position(call.Rparen).Line
	if start == end ||
		lineIndentAt(fset, lines, call.Pos(), tab) !=
			lineIndentAt(fset, lines, call.Rparen, tab) {

		return false
	}
	found := false
	line := start
	indent := lineIndentAt(fset, lines, call.Pos(), tab)
	for _, arg := range call.Args {
		first := fset.Position(arg.Pos()).Line
		last := fset.Position(arg.End()).Line
		if first != line {
			return false
		}
		if first == last {
			continue
		}
		if lineIndentAt(fset, lines, arg.End(), tab) != indent {
			return false
		}
		found = true
		line = last
		for {
			u, ok := arg.(*ast.UnaryExpr)
			if !ok {
				break
			}
			arg = u.X
		}
		switch x := arg.(type) {
		case *ast.CallExpr:
			if allowWrappedNested {
				if !validCallLayout(ctx, x, tab) {
					return false
				}
			} else if !inlineSymmetricCall(ctx, x, tab, false) {
				return false
			}

		case *ast.CompositeLit:
			if fset.Position(containerOpening(
				fset, x,
			)).Line != first {

				return false
			}
			literalLine := fset.Position(x.Lbrace).Line
			if literalLine != first && lineIndentAt(
				fset, lines, x.Lbrace, tab,
			) != indent {

				return false
			}
			for _, elt := range x.Elts {
				if literalLine != last &&
					fset.Position(elt.End()).Line >= last {

					return false
				}
			}

		case *ast.FuncLit:
			if x.Body == nil ||
				fset.Position(x.Body.Lbrace).Line != first {

				return false
			}
			for _, stmt := range x.Body.List {
				if fset.Position(stmt.End()).Line >= last {
					return false
				}
			}

		default:
			return false
		}
	}
	return found && line == end
}

// promotableContainer reports whether expr is a single-line call / composite
// (or &composite) that R6's promote-step can convert to a multi-line
// container. Already-multi-line containers are NOT promotable here — they're
// handled by step 2's "existing container" branch. Returns the inner expr
// to promote (unwraps UnaryExpr "&T{...}" to T{...}'s composite, since
// applyMultiLine targets the composite).
func promotableContainer(expr dst.Expr) (dst.Expr, bool) {
	if isMultiLineContainer(expr) {
		return nil, false
	}
	switch x := expr.(type) {
	case *dst.CallExpr:
		if len(x.Args) > 0 {
			return x, true
		}

	case *dst.CompositeLit:
		if len(x.Elts) > 0 {
			return x, true
		}

	case *dst.UnaryExpr:
		return promotableContainer(x.X)
	}
	return nil, false
}

// promotedContainerFits checks that every inner arg / elt of a container
// would fit on its own continuation line at the given indent, plus a
// trailing comma. A conservative check — actual layout may pack multiple
// per line and fit even when this returns false, but we prefer correctness
// over aggressiveness: a failing check means promotion is rejected and R4
// falls back to layoutPack, which is always safe.
func promotedContainerFits(expr ast.Expr, contIndent, limit int,
	fset *token.FileSet, lines [][]byte, tab int) bool {

	switch x := expr.(type) {
	case *ast.CallExpr:
		ws := argWidths(fset, lines, x.Args, tab)
		for _, w := range ws {
			if w >= wideForcedBreak {
				// An inner arg is itself multi-line in source;
				// we can't measure its post-promote width
				// reliably, so refuse the promotion.
				return false
			}
			if contIndent+w+1 > limit {
				return false
			}
		}
		return true

	case *ast.CompositeLit:
		ws := argWidths(fset, lines, x.Elts, tab)
		for _, w := range ws {
			if w >= wideForcedBreak {
				return false
			}
			if contIndent+w+1 > limit {
				return false
			}
		}
		return true

	case *ast.UnaryExpr:
		return promotedContainerFits(
			x.X, contIndent, limit, fset, lines, tab,
		)
	}
	return false
}

// promoteToMultiline turns a single-line container into a multi-line one
// for R6's symmetric layout. The inner content is laid out the same way
// R4 / R7 would lay it out on its own — packed onto continuation lines
// for call args and non-keyed (slice) composites, one element per line
// for keyed (struct / map) composites — so the result reads as the
// container's "own" multi-line form, just opened from the outer call's
// arg line. The last inner element gets After=NewLine so the container's
// close token rides the outer call's closing ")" on a shared line (the
// "})" / "))" pattern).
func promoteToMultiline(ctx *Context, cand dst.Expr, astCand ast.Expr,
	contIndent, limit, tab int) {

	fset, lines := ctx.FileSet, ctx.SourceLines
	switch x := cand.(type) {
	case *dst.CallExpr:
		ac, ok := astCand.(*ast.CallExpr)
		if !ok || len(x.Args) == 0 {
			return
		}
		breaks := packCallArgs(ctx, ac, x, contIndent, limit, tab)
		for _, arg := range x.Args {
			ctx.ReflowedArgs[arg] = true
		}
		applyCallLayout(x, breaks, true)

	case *dst.CompositeLit:
		ac, ok := astCand.(*ast.CompositeLit)
		if !ok || len(x.Elts) == 0 {
			return
		}
		if isKeyedComposite(x) {
			// Struct / map: one field per line (R7 convention —
			// packed struct fields ruin git-diff hygiene).
			for _, e := range x.Elts {
				e.Decorations().Before = dst.NewLine
			}
			x.Elts[len(x.Elts)-1].Decorations().After = dst.NewLine
			return
		}
		// Slice / array: greedy pack like R7's applySliceReflow.
		widths := argWidths(fset, lines, ac.Elts, tab)
		contBudget := limit - contIndent
		breaks := packLayout(widths, contBudget, contBudget, 1)
		clearArgDecorations(x.Elts)
		x.Elts[0].Decorations().Before = dst.NewLine
		for _, i := range breaks {
			if i >= 0 && i < len(x.Elts) {
				x.Elts[i].Decorations().Before = dst.NewLine
			}
		}
		x.Elts[len(x.Elts)-1].Decorations().After = dst.NewLine

	case *dst.UnaryExpr:
		au, ok := astCand.(*ast.UnaryExpr)
		if !ok {
			return
		}
		promoteToMultiline(
			ctx, x.X, au.X, contIndent, limit, tab,
		)
	}
}

// containerOpening locates the first container that can introduce a line
// break. An anonymous type can open before the composite's value literal:
// struct { ... }{ ... } starts at the type's brace, not the value's brace.
func containerOpening(fset *token.FileSet, expr ast.Expr) token.Pos {
	switch x := expr.(type) {
	case *ast.CompositeLit:
		opening := x.Lbrace
		if x.Type != nil && fset.Position(x.Pos()).Line !=
			fset.Position(x.Lbrace).Line {

			// Inspect the type so arrays and maps containing an
			// anonymous struct type use the same opening rule.
			ast.Inspect(x.Type, func(n ast.Node) bool {
				var fields *ast.FieldList
				switch t := n.(type) {
				case *ast.StructType:
					fields = t.Fields

				case *ast.InterfaceType:
					fields = t.Methods
				}
				if fields != nil && fields.Opening.IsValid() &&
					fields.Opening < opening {

					opening = fields.Opening
				}
				return true
			})
		}
		return opening

	case *ast.CallExpr:
		return x.Lparen

	case *ast.FuncLit:
		if x.Body != nil {
			return x.Body.Lbrace
		}

	case *ast.UnaryExpr:
		return containerOpening(fset, x.X)
	}
	return token.NoPos
}

// openTokenWidth returns the visual width of an expression's "opening token"
// — the prefix up to and including the first `{` or `(` that introduces a
// multi-line body. For &Foo{...} the open token is "&Foo{"; for f(x, y) it's
// "f("; for `func(a int) bool {` it's the whole header. Returns 0 if the
// expression has no recognisable open token (i.e. isMultiLineContainer would
// return false).
func openTokenWidth(fset *token.FileSet, lines [][]byte, expr ast.Expr,
	tab int) int {

	lbrace := containerOpening(fset, expr)
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

// clearCallArgLayout clears wrapping around arguments, including a newline
// after the variadic ellipsis. Comments remain attached to their tokens.
func clearCallArgLayout(call *dst.CallExpr) {
	clearArgDecorations(call.Args)
	decs := call.Decs.Ellipsis[:0]
	for _, dec := range call.Decs.Ellipsis {
		if dec != "\n" {
			decs = append(decs, dec)
		}
	}
	call.Decs.Ellipsis = decs
}

func applyCallLayout(call *dst.CallExpr, breaks []int, multiLine bool) {
	clearCallArgLayout(call)
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
	// gofmt translates to "trailing comma + ) on next line". A variadic
	// call needs the break after '...', which follows the last arg node.
	if call.Ellipsis {
		call.Decs.Ellipsis.Append("\n")
	} else {
		call.Args[len(call.Args)-1].Decorations().After = dst.NewLine
	}
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

	// Assertions put operands before the message. R5's usual first-string
	// convention must not split an expected value instead of that message.
	if index, ok := requireMessageIndex(ctx.AstFile, astCall); ok {
		return applyRequireFormattingLayout(
			ctx, astCall, call, index, limit, tab,
		)
	}

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

// allCallLinesFit reports whether every source line of a call's OWN argument
// layout is within the limit. Lines that fall in the INTERIOR of a multi-line
// argument (a func literal's body, a composite literal's elements, a nested
// multi-line call) are excluded: those belong to other passes (R3/R7/recursive
// R4), not to this call's framing. Without that exclusion, one long line deep
// inside a closure body would force R4 to re-wrap the OUTER call — shifting the
// whole body a tab deeper and cascading new over-limit lines (a non-idempotent
// churn). The call's own framing lines (its opening, top-level arg lines, and
// closing) are still checked.
func allCallLinesFit(ctx *Context, call *ast.CallExpr, limit, tab int) bool {
	fset := ctx.FileSet
	startLine := fset.Position(call.Pos()).Line
	endLine := fset.Position(call.End()).Line

	// Collect the line ranges occupied by multi-line args. R4 controls only
	// WHERE an arg begins, never the width of the arg's own body lines, so
	// a multi-line arg's entire span is excluded from this call's overrun
	// check (an over-limit line inside a wrapped condition / closure /
	// nested call is some other pass's problem, or irreducible —
	// re-wrapping THIS call can't fix it and would only churn). The call's
	// own framing — its opening and closing lines — is never excluded, so
	// an arg sharing those lines is still accounted for.
	type lineRange struct{ lo, hi int }
	var argSpans []lineRange
	argBoundaries := make(map[int]int)
	for _, a := range call.Args {
		s := fset.Position(a.Pos()).Line
		e := fset.Position(a.End()).Line
		argBoundaries[s]++
		if e > s {
			argBoundaries[e]++
			argSpans = append(argSpans, lineRange{s, e})
		}
	}
	inArgBody := func(ln int) bool {
		// A line shared by arguments belongs to this call's packing
		// even if it also opens or closes a multi-line container.
		if ln == startLine || ln == endLine || argBoundaries[ln] > 1 {
			return false
		}
		for _, r := range argSpans {
			if ln >= r.lo && ln <= r.hi {
				return true
			}
		}
		return false
	}

	for ln := startLine; ln <= endLine; ln++ {
		if inArgBody(ln) {
			continue
		}
		if ln <= 0 || ln > len(ctx.SourceLines) {
			return false
		}
		src := ctx.SourceLines[ln-1]
		if visualWidth(src, tab) > limit && !hasNolintLL(src) {
			return false
		}
	}
	return true
}

// hasNolintLL reports whether a source line carries a "//nolint" directive that
// disables the line-length linter (bare //nolint or //nolint:...,ll). Such a
// line is intentionally over-limit, so layout passes must not treat it as an
// overrun and re-wrap the surrounding construct on its account — that just
// churns compliant code around an explicitly-exempted line.
func hasNolintLL(line []byte) bool {
	i := bytes.Index(line, []byte("//nolint"))
	if i < 0 {
		return false
	}
	rest := line[i+len("//nolint"):]

	// Bare "//nolint" (optionally spaced) disables all linters, incl. ll.
	if len(rest) == 0 || rest[0] != ':' {
		return true
	}
	return bytes.Contains(rest, []byte("ll"))
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
