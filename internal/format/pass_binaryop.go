package format

import (
	"go/ast"
	"go/token"

	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// binaryOpWrap implements R16: when a statement's source line exceeds the limit
// and its outer expression is a binary operator chain (&&, ||, +, -, *, /, &,
// |, ^), prefer breaking AFTER the binary operator over wrapping the operand
// sub-expressions individually. Reads more naturally —
//
//	if strings.ContainsRune(ch, '\\') &&
//	    !strings.ContainsRune(ch, ' ') {
//
// beats
//
//	if strings.ContainsRune(
//	    ch, '\\',
//	) && !strings.ContainsRune(ch, ' ') {
//
// R16 runs BEFORE R4 and marks every inner CallExpr in the broken binary as
// OuterHandled, so R4 leaves the operand calls alone.
type binaryOpWrap struct{}

func (binaryOpWrap) Name() string { return "R16" }

// stmtBinaryLinesFit reports whether every source line from the statement start
// through the end of the binary expression is within the limit. When true, the
// author's existing operator layout is structurally valid and must be left
// untouched in the default (non-optimize) mode.
func stmtBinaryLinesFit(ctx *Context, stmt ast.Node, bin *ast.BinaryExpr,
	limit, tab int) bool {

	start := ctx.FileSet.Position(stmt.Pos()).Line
	end := ctx.FileSet.Position(bin.End()).Line
	if end < start {
		start, end = end, start
	}
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

func (binaryOpWrap) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.BinaryOpWrapOn() {
		return nil
	}
	limit := ctx.Config.LineLength
	tab := ctx.Config.TabWidth
	if tab <= 0 {
		tab = 8
	}
	parents := buildDstParents(ctx.File)

	dst.Inspect(ctx.File, func(n dst.Node) bool {
		if ctx.SkipNolintDecl(n) {
			return false
		}
		slot := binaryOpSlot(n)
		if slot == nil {
			return true
		}
		bin, ok := (*slot).(*dst.BinaryExpr)
		if !ok {
			return true
		}
		if !canSplitOp(bin.Op) {
			return true
		}

		// Pure string-concatenation chains ("a" + "b" + ...) are owned
		// by R9, which splits/joins at string-body boundaries. R16's
		// operator breaks here conflict with R9 and, when an operand
		// can't be split further (a no-space literal), R16's fitK<0
		// fallback collapses the chain onto an over-limit line that R9
		// then can't fix — a churny, non-idempotent result. Leave
		// string concats to R9.
		if isStringExpr(bin) {
			return true
		}
		astN, ok := ctx.Decorator.Ast.Nodes[n]
		if !ok {
			return true
		}
		astStmt, isNode := astN.(ast.Node)
		if !isNode {
			return true
		}
		astBinN, ok := ctx.Decorator.Ast.Nodes[bin]
		if !ok {
			return true
		}
		astBin, ok := astBinN.(*ast.BinaryExpr)
		if !ok {
			return true
		}

		// Skip when the enclosing block is single-line in source —
		// R12 will body-split it, so source widths here describe the
		// body-collapsed form ("func F() T { stmt }"), not the post-R12
		// statement line. Letting R16 split operators in that case
		// would over-wrap statements that ultimately fit.
		if enclosingBlockIsSingleLine(ctx, n, parents) {
			return true
		}

		// HARD-only by default: a binary chain is re-broken solely to
		// resolve an over-limit line. If the statement's current source
		// lines (from its start through the binary expression) already
		// fit, the author's operator layout is valid — leave it.
		// Repacking a fitting chain is a SOFT, --optimize-only change.
		if !ctx.Config.Optimize &&
			stmtBinaryLinesFit(ctx, astStmt, astBin, limit, tab) {

			return true
		}

		renderedIndent := stmtRenderedIndent(ctx, n, parents, tab)
		contIndent := renderedIndent + tab

		// Prefix: source width from the statement keyword/LHS to the
		// start of the slot expression. Covers "if ", "return ",
		// "x := ", "if init; " uniformly. For multi-line prefixes
		// (rare — e.g. an `if init` with init wrapped across lines)
		// we can't reliably estimate the joined width, so bail.
		prefixW := sourceWidth(
			ctx.FileSet, ctx.SourceLines,
			astStmt.Pos(), astBin.Pos(), tab,
		)
		if prefixW >= wideForcedBreak {
			return true
		}

		// Suffix: for control-flow statements, source from the slot
		// end to one past the body's "{". Value statements (return,
		// assign, expr) have no trailing header text.
		suffixW := 0
		if hdrEnd := stmtHeaderEnd(astStmt); hdrEnd.IsValid() {
			sw := sourceWidth(
				ctx.FileSet, ctx.SourceLines,
				astBin.End(), hdrEnd, tab,
			)
			if sw < wideForcedBreak {
				suffixW = sw
			}
		}

		// Collect the left spine of same-operator BinaryExprs.
		// `A op B op C op D` parses as `(((A op B) op C) op D)`;
		// dstChain[0] is the outermost, dstChain[N-1] the innermost.
		// astChain mirrors dstChain for source-position lookups.
		dstChain, astChain := collectBinaryChain(bin, astBin)
		N := len(dstChain) // number of operators
		opW := len(astBin.Op.String())

		// Operand widths in source order:
		//   operands[0]   = innermost.X  (chain[N-1].X)
		//   operands[i>0] = chain[N-i].Y
		operandW := make([]int, N+1)
		operandW[0] = astExprJoinedWidth(
			ctx.FileSet, ctx.SourceLines,
			astChain[N-1].X, tab,
		)
		for i := 1; i <= N; i++ {
			operandW[i] = astExprJoinedWidth(
				ctx.FileSet, ctx.SourceLines,
				astChain[N-i].Y, tab,
			)
		}

		// Greedy-pack operands across continuation lines, preferring
		// fewer lines. The "always-one-per-line" form is wrong when
		// short trailing operands could ride a previous continuation:
		// `1 + 1 + len(x) + len(y) + 32 + len(z) + 32` should break
		// to two lines, not three.
		breaks, ok := packBinaryChain(
			operandW, renderedIndent+prefixW,
			contIndent, opW, suffixW, limit,
		)
		if !ok {
			// Some operand alone exceeds its line budget; no
			// operator break helps. Clear any stale breaks and
			// let R4 wrap inner calls instead.
			applyChainBreaks(dstChain, nil)
			return true
		}

		applyChainBreaks(dstChain, breaks)
		if len(breaks) > 0 {
			// At least one operator broken — keep operand calls
			// off R4's hands (their source positions are no
			// longer authoritative for layout decisions).
			clearInnerCallWraps(bin)
			markBinaryInnerCallsHandled(ctx.OuterHandled, bin)
		}
		return true
	})
	return nil
}

// collectBinaryChain returns the left spine of same-operator BinaryExprs in
// outermost-first order. Both dst and ast chains are returned in parallel
// so callers can consult source positions while mutating decorations.
func collectBinaryChain(bin *dst.BinaryExpr,
	astBin *ast.BinaryExpr) ([]*dst.BinaryExpr, []*ast.BinaryExpr) {

	dstChain := []*dst.BinaryExpr{bin}
	astChain := []*ast.BinaryExpr{astBin}
	curDst := bin
	curAst := astBin
	for {
		leftDst, okD := curDst.X.(*dst.BinaryExpr)
		leftAst, okA := curAst.X.(*ast.BinaryExpr)
		if !okD || !okA {
			break
		}
		if leftDst.Op != bin.Op || leftAst.Op != astBin.Op {
			break
		}
		dstChain = append(dstChain, leftDst)
		astChain = append(astChain, leftAst)
		curDst = leftDst
		curAst = leftAst
	}
	return dstChain, astChain
}

// packBinaryChain greedily packs operands across continuation lines and
// returns the list of operand indices (1..N) before which a line break is
// inserted. It always reserves room on each non-last line for a trailing
// " op" (so the line can validly close on a break point); the last line
// reserves suffixW instead. Returns (nil, false) when no packing makes
// every line fit — typically an operand alone exceeds its line budget,
// in which case operator breaks can't help and R4 should wrap inner
// calls.
//
// The classic illustration of why greedy packing matters: for
// `1 + 1 + len(x) + len(y) + 32 + len(z) + 32` we want
//
//	1 + 1 + len(x) + len(y) + 32 +
//	    len(z) + 32
//
// not
//
//	1 + 1 + len(x) + len(y) + 32 +
//	    len(z) +
//	    32
//
// The trailing `32` is short enough to ride line 2's continuation; an
// "always one operand per line" policy would split it off unnecessarily.
func packBinaryChain(operandW []int,
	firstLineStart, contLineStart, opW, suffixW, limit int) ([]int, bool) {

	N := len(operandW) - 1 // number of operators
	if N < 1 {
		return nil, true
	}

	var breaks []int
	curWidth := firstLineStart + operandW[0]
	for i := 1; i <= N; i++ {
		isLast := i == N

		// Cost of appending " op " + operand[i] to the current line.
		addW := opW + 2 + operandW[i]

		// Reserved ending: " op" on a non-last line (in case the
		// NEXT operand has to break, leaving this operand last on
		// the line); suffix on the last line.
		endingW := opW + 1
		if isLast {
			endingW = suffixW
		}

		if curWidth+addW+endingW <= limit {
			curWidth += addW
			continue
		}

		// Doesn't fit — break BEFORE operand[i]. The current line
		// ends with the trailing " op" (already budgeted).
		breaks = append(breaks, i)
		curWidth = contLineStart + operandW[i]
		if curWidth+endingW > limit {
			// Operand[i] alone wouldn't even fit on its own
			// line with the required ending. Greedy can't
			// succeed.
			return nil, false
		}
	}
	return breaks, true
}

// applyChainBreaks sets dst.NewLine on the Y operand of every chain
// element identified by an operand index in breaks, and clears the
// rest. Chain indexing inverts operand order — for N+1 operands, chain
// element c.Y holds operand index (N-c), so a break before operand i
// becomes a NewLine on dstChain[N-i].Y.
func applyChainBreaks(dstChain []*dst.BinaryExpr, breaks []int) {
	N := len(dstChain)
	want := make(map[int]bool, len(breaks))
	for _, b := range breaks {
		idx := N - b
		if idx >= 0 && idx < N {
			want[idx] = true
		}
	}
	for i, bin := range dstChain {
		if want[i] {
			bin.Y.Decorations().Before = dst.NewLine
		} else {
			clearOpBreak(bin)
		}
	}
}

// binaryLinesFit reports whether splitting the chain at its LAST k operators
// produces lines that all fit within limit. Retained for any callers that
// still want the k-based view; the active R16 logic uses packBinaryChain
// instead, which can pack multiple operands per continuation line.
func binaryLinesFit(operandW []int,
	k, renderedIndent, contIndent,
	prefixW, opW, suffixW, limit int) bool {

	N := len(operandW) - 1 // number of operators
	if k < 0 || k > N {
		return false
	}

	// Line 1: operands[0..N-k] with N-k inter-operand " op " gaps,
	// followed by either suffix (k==0) or trailing " op" (k>=1).
	sumLine1 := 0
	for j := 0; j <= N-k; j++ {
		sumLine1 += operandW[j]
	}
	line1 := renderedIndent + prefixW + sumLine1 + (N-k)*(opW+2)
	if k == 0 {
		line1 += suffixW
	} else {
		line1 += opW + 1 // trailing " op"
	}
	if line1 > limit {
		return false
	}

	// Lines 2..k+1: one operand per line, trailing " op" except last.
	for i := 1; i <= k; i++ {
		w := contIndent + operandW[N-k+i]
		if i < k {
			w += opW + 1
		} else {
			w += suffixW
		}
		if w > limit {
			return false
		}
	}
	return true
}

// applyChainSplits sets dst.NewLine on the right operand of the first k
// chain elements (outermost-to-inner) and clears the rest. The chain's
// inner CallExpr wraps stay untouched here — callers handle that when
// they want to prevent R4 from re-wrapping operand calls.
func applyChainSplits(dstChain []*dst.BinaryExpr, k int) {
	for i, bin := range dstChain {
		if i < k {
			bin.Y.Decorations().Before = dst.NewLine
		} else {
			clearOpBreak(bin)
		}
	}
}

// clearOpBreak removes a NewLine/EmptyLine Before-decoration on the
// binary expression's right operand. Used when R16 has decided the
// expression should NOT carry an operator break — either the joined
// form fits, or the operator split wouldn't help and R4 should own
// the wrap instead.
func clearOpBreak(bin *dst.BinaryExpr) {
	d := bin.Y.Decorations()
	if d == nil {
		return
	}
	if d.Before == dst.NewLine || d.Before == dst.EmptyLine {
		d.Before = dst.None
	}
}

// stmtHeaderEnd returns the position one byte past the body's "{" for a
// control-flow statement, or NoPos for value statements (the caller falls back
// to ast.Node.End() in that case).
func stmtHeaderEnd(n ast.Node) token.Pos {
	switch s := n.(type) {
	case *ast.IfStmt:
		if s.Body != nil {
			return s.Body.Lbrace + 1
		}

	case *ast.ForStmt:
		if s.Body != nil {
			return s.Body.Lbrace + 1
		}

	case *ast.RangeStmt:
		if s.Body != nil {
			return s.Body.Lbrace + 1
		}

	case *ast.SwitchStmt:
		if s.Body != nil {
			return s.Body.Lbrace + 1
		}

	case *ast.TypeSwitchStmt:
		if s.Body != nil {
			return s.Body.Lbrace + 1
		}
	}
	return token.NoPos
}

// enclosingBlockIsSingleLine reports whether the nearest enclosing dst
// BlockStmt is single-line in source (lbrace.Line == rbrace.Line). When it is,
// R12 will body-split it and the source line containing this statement is the
// body-collapsed form ("func F() T { stmt }") — not a reliable proxy for the
// post-split statement line.
func enclosingBlockIsSingleLine(ctx *Context, dn dst.Node,
	parents map[dst.Node]dst.Node) bool {

	cur := dn
	for {
		p, ok := parents[cur]
		if !ok || p == nil {
			return false
		}
		if blk, isBlk := p.(*dst.BlockStmt); isBlk {
			astN := ctx.Decorator.Ast.Nodes[blk]
			abs, isBs := astN.(*ast.BlockStmt)
			if !isBs {
				return false
			}
			return isSingleLine(ctx.FileSet, abs.Lbrace, abs.Rbrace)
		}
		cur = p
	}
}

// binaryOpSlot returns a pointer to the dst expression slot inside the
// statement that R16 should consider for binary-op splitting. Slots are the
// "outermost expression position" — Cond for control-flow headers, the lone
// Result for a single-value return, the lone RHS of an assignment, the X of an
// ExprStmt. Returns nil when the statement has no eligible slot.
func binaryOpSlot(n dst.Node) *dst.Expr {
	switch s := n.(type) {
	case *dst.IfStmt:
		if s.Cond != nil {
			return &s.Cond
		}

	case *dst.ForStmt:
		if s.Cond != nil {
			return &s.Cond
		}

	case *dst.ReturnStmt:
		if len(s.Results) == 1 {
			return &s.Results[0]
		}

	case *dst.AssignStmt:
		if len(s.Rhs) == 1 {
			return &s.Rhs[0]
		}

	case *dst.ExprStmt:
		return &s.X
	}
	return nil
}

// canSplitOp reports whether the operator is worth breaking after. Logical and
// arithmetic / bitwise binops are; comparison ops (==, !=, <, …) are not —
// comparisons usually appear inside a logical chain, and breaking at the
// comparison would orphan one operand.
func canSplitOp(op token.Token) bool {
	switch op {
	case token.LAND, token.LOR,
		token.ADD, token.SUB,
		token.MUL, token.QUO, token.REM,
		token.AND, token.OR, token.XOR,
		token.SHL, token.SHR, token.AND_NOT:

		return true
	}
	return false
}

// markBinaryInnerCallsHandled marks every CallExpr inside a binary expression
// as OuterHandled, so R4 doesn't wrap its operand calls when R16 has already
// broken the binary at the operator.
func markBinaryInnerCallsHandled(handled map[*dst.CallExpr]bool,
	bin *dst.BinaryExpr) {

	dst.Inspect(bin, func(n dst.Node) bool {
		if c, ok := n.(*dst.CallExpr); ok {
			handled[c] = true
		}
		return true
	})
}

// clearInnerCallWraps clears NewLine/EmptyLine Before decorations on call args
// inside a binary expression. When R16 reflows a previously call-arg-wrapped
// expression to operator-split form, those stale wraps need to go or the
// printer keeps them around — leaving an awkward mix of operator split AND
// call-arg wraps.
func clearInnerCallWraps(bin *dst.BinaryExpr) {
	dst.Inspect(bin, func(n dst.Node) bool {
		c, ok := n.(*dst.CallExpr)
		if !ok {
			return true
		}
		for _, a := range c.Args {
			d := a.Decorations()
			if d == nil {
				continue
			}
			if d.Before == dst.NewLine ||
				d.Before == dst.EmptyLine {

				d.Before = dst.None
			}
			if d.After == dst.NewLine || d.After == dst.EmptyLine {
				d.After = dst.None
			}
		}
		return true
	})
}

// stmtRenderedIndent returns the visual indent at which the statement would
// render — its enclosing BlockStmt's owner indent + tab. The owner is the
// FuncDecl / FuncLit / IfStmt / ForStmt / etc. that introduces the block;
// its FIRST source line is the right anchor for the block's indent. Using
// the block's Lbrace line instead would over-count by one tab whenever the
// owner has a multi-line header (the Lbrace then lives on a continuation
// line at owner-indent + tab).
func stmtRenderedIndent(ctx *Context, dn dst.Node,
	parents map[dst.Node]dst.Node, tab int) int {

	cur := dn
	for {
		p, ok := parents[cur]
		if !ok || p == nil {
			return 0
		}
		if _, isBlk := p.(*dst.BlockStmt); !isBlk {
			cur = p
			continue
		}

		// Found enclosing block; the block's parent in the dst tree
		// is the owner (FuncDecl / IfStmt / for ...). Use its first
		// source line as the indent anchor.
		owner, hasOwner := parents[p]
		if !hasOwner || owner == nil {
			return 0
		}
		astN, hasAst := ctx.Decorator.Ast.Nodes[owner]
		if !hasAst {
			return 0
		}
		ownerNode, isNode := astN.(ast.Node)
		if !isNode {
			return 0
		}
		return lineIndentAt(
			ctx.FileSet, ctx.SourceLines, ownerNode.Pos(), tab,
		) + tab
	}
}

// astExprJoinedWidth returns the visual width of expr if every source-
// multi-line span inside it were collapsed onto a single line. Used by R16 to
// ask "if this expression were joined, would it fit?" — distinct from
// sourceWidth, which returns wideForcedBreak the moment a span crosses a line
// boundary.
func astExprJoinedWidth(fset *token.FileSet, lines [][]byte, e ast.Expr,
	tab int) int {

	sp := fset.Position(e.Pos())
	ep := fset.Position(e.End())
	if sp.Line == ep.Line {
		return sourceWidth(fset, lines, e.Pos(), e.End(), tab)
	}
	switch x := e.(type) {
	case *ast.BinaryExpr:
		return astExprJoinedWidth(fset, lines, x.X, tab) + 1 +
			len(x.Op.String()) +
			1 +
			astExprJoinedWidth(fset, lines, x.Y, tab)

	case *ast.CallExpr:
		w := astExprJoinedWidth(fset, lines, x.Fun, tab) + 2 // "()"
		for i, arg := range x.Args {
			if i > 0 {
				w += 2 // ", "
			}
			w += astExprJoinedWidth(fset, lines, arg, tab)
		}
		return w

	case *ast.UnaryExpr:
		return len(x.Op.String()) +
			astExprJoinedWidth(fset, lines, x.X, tab)

	case *ast.ParenExpr:
		return 2 + astExprJoinedWidth(fset, lines, x.X, tab)

	case *ast.SelectorExpr:
		return astExprJoinedWidth(fset, lines, x.X, tab) + 1 +
			len(x.Sel.Name)

	case *ast.StarExpr:
		return 1 + astExprJoinedWidth(fset, lines, x.X, tab)

	case *ast.IndexExpr:
		return astExprJoinedWidth(fset, lines, x.X, tab) + 2 +
			astExprJoinedWidth(fset, lines, x.Index, tab)

	case *ast.TypeAssertExpr:
		w := astExprJoinedWidth(fset, lines, x.X, tab) + 3
		if x.Type != nil {
			w += astExprJoinedWidth(fset, lines, x.Type, tab)
		}
		return w
	}

	// Fallback for nodes R16 doesn't know how to descend into
	// (CompositeLit, FuncLit, SliceExpr, ...): sum trimmed line widths
	// between the start and end positions.
	return summedTrimmedSpan(lines, sp, ep, tab)
}

// summedTrimmedSpan returns the sum of visual widths of the source-line
// fragments that make up an expression's span, with each non-first line's
// leading whitespace trimmed. Approximate but adequate for the rare nodes R16
// falls back to.
func summedTrimmedSpan(lines [][]byte, sp, ep token.Position, tab int) int {
	if sp.Line < 1 || ep.Line > len(lines) {
		return 0
	}
	w := 0
	for ln := sp.Line; ln <= ep.Line; ln++ {
		line := lines[ln-1]
		startCol, endCol := 0, len(line)
		if ln == sp.Line {
			startCol = sp.Column - 1
		} else {
			for startCol < len(line) && (line[startCol] == ' ' ||
				line[startCol] == '\t') {

				startCol++
			}
		}
		if ln == ep.Line {
			endCol = ep.Column - 1
		}
		if startCol < 0 {
			startCol = 0
		}
		if endCol > len(line) {
			endCol = len(line)
		}
		if startCol < endCol {
			w += visualWidth(line[startCol:endCol], tab)
		}
	}
	return w
}
