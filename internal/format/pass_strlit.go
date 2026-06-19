package format

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"

	"github.com/guggero/goformat/internal/diag"
)

// stringLitWrap implements R9 as a string-reflow rule: it walks every outermost
// string expression (a lone string literal OR a `"a" + "b" [+ "c" …]` concat
// chain of string literals), gathers the chunks into one logical body, and
// re-splits the body into N chunks sized to fill each line up to the limit.
// Both single literals on overlong lines and existing concat chains with
// suboptimal split positions route through the same code path — that's what
// gives the user the "reflow" they asked for.
//
// Multi-split: keep splitting until the rest fits a continuation line. Prefer
// the rightmost space within the budget; fall back to the exact budget for
// strings without internal spaces (hex, identifiers).
//
// Effective indent: when an enclosing call has been wrapped by R4, the string
// lives on a continuation line at the call's indent + one tab, not at its
// source column. We walk dst parents looking for wrapped CallExprs (recognised
// by NewLine on args[0]) and adjust accordingly. Subsequent chunks land one
// additional tab deeper — gofmt's continuation rule for multi-line binary
// expressions.
//
// Scope:
//   - Only interpreted "..." literals; backtick raw strings are left
//     alone.
//   - Only literals whose content has no backslash escapes (splitting
//     mid-escape would corrupt the value).
//   - Calls in the formatting-funcs allowlist (fmt.Errorf, log.Debugf,
//     …) are skipped here — R5 owns their compact-wrap behaviour.
type stringLitWrap struct{}

func (stringLitWrap) Name() string { return "R9" }

func (stringLitWrap) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.StringLitWrapOn() {
		return nil
	}
	cfg := ctx.Config
	tab := cfg.TabWidth
	if tab <= 0 {
		tab = 8
	}
	limit := cfg.LineLength
	if limit <= 0 {
		limit = 80
	}
	fmtFns := cfg.FormattingFuncs
	denyFns := cfg.FormattingFuncsDeny

	parents := buildDstParents(ctx.File)

	dstutil.Apply(ctx.File, func(c *dstutil.Cursor) bool {
		node := c.Node()
		expr, ok := node.(dst.Expr)
		if !ok || !isStringExpr(expr) {
			return true
		}

		// Process only the outermost member of a string-concat chain;
		// the inner BinaryExprs / BasicLits will be replaced wholesale
		// when we rewrite the outer node.
		if p := parents[node]; p != nil {
			if pe, ok := p.(dst.Expr); ok && isStringExpr(pe) {
				return false
			}
		}

		// Hand off formatting-style calls to R5.
		if insideFormattingCall(ctx, node, parents, fmtFns, denyFns) {
			return true
		}

		// Decide whether to reflow.
		//   * BinaryExpr concat: always (it's an existing
		//     concatenation; we re-evaluate its split positions).
		//   * Single BasicLit: only if its source line was already
		//     over the limit (prep marked it). Touching short
		//     unproblematic strings everywhere would be a regression.
		_, isConcat := expr.(*dst.BinaryExpr)
		if isConcat {
			// An existing concat inside a layout-fragile parent
			// (a keyed entry of a CompositeLit) is left alone
			// when every source line already fits: rejoining can
			// push the line over the limit via gofmt's alignment
			// padding, and the next run re-splits — oscillation.
			astN, ok := ctx.Decorator.Ast.Nodes[expr]
			if ok {
				if astExpr, ok := astN.(ast.Expr); ok && isMultiLineAndAllLinesFit(
					ctx, astExpr, limit, tab,
				) && insideLayoutFragileParent(
					parents, expr,
				) {

					return true
				}
			}
		} else {
			lit := expr.(*dst.BasicLit)
			if _, marked := ctx.StringsToSplit[lit]; !marked {
				return true
			}
		}

		var chunks []string
		if !collectStringChunks(expr, &chunks) || len(chunks) == 0 {
			return true
		}

		// Skip only if a chunk contains a backslash AND has no space
		// — for space-bearing strings the splitter only ever cuts at
		// space boundaries, which leaves any escape sequence intact.
		// Pure no-space backslash bodies would otherwise risk a
		// budget-position split landing mid-escape.
		for _, ch := range chunks {
			if strings.ContainsRune(ch, '\\') &&
				!strings.ContainsRune(ch, ' ') {

				return true
			}
		}
		body := strings.Join(chunks, "")

		leftmost := leftmostStringLit(expr)
		if leftmost == nil {
			return true
		}
		srcQuoteCol, srcLineIndent := sourcePosOfLit(ctx, leftmost, tab)

		wraps := countWrappedAncestors(node, parents)
		var firstQuoteCol, firstLineIndent int
		if wraps > 0 {
			firstLineIndent = srcLineIndent + wraps*tab
			firstQuoteCol = firstLineIndent
		} else {
			firstLineIndent = srcLineIndent
			firstQuoteCol = srcQuoteCol
		}
		contIndent := firstLineIndent + tab

		// Budgets — frame around the body content:
		//   non-last chunk: "<body>" + (4 chars)
		//   last chunk:     "<body>",  (3 chars; covers trailing
		//                                comma in a multi-line call
		//                                arg or end-of-statement)
		firstNonLast := limit - firstQuoteCol - 4
		firstLast := limit - firstQuoteCol - 3
		contNonLast := limit - contIndent - 4
		contLast := limit - contIndent - 3

		if firstNonLast < 2 || contNonLast < 2 {
			return true
		}

		// When an existing concat collapses to a body that fits whole
		// on a continuation line — and we sit inside a multi-line
		// container (slice composite, struct composite, wrapped call)
		// where pushing onto a fresh line is natural — emit a single
		// literal with Before=NewLine instead of mid-word splitting.
		// The container's existing per-element layout already
		// establishes that one element per line is acceptable, so a
		// moved-not-split string reads better than "Crit"
		// + "icalS".
		if isConcat && len(body) <= contLast && len(body) > firstLast &&
			inMultilineContainer(node, parents) {

			repl := &dst.BasicLit{
				Kind:  token.STRING,
				Value: `"` + body + `"`,
			}
			copyOuterDecs(node, repl)
			repl.Decs.Before = dst.NewLine
			c.Replace(repl)
			return false
		}

		newChunks := multiSplit(
			body, firstNonLast, firstLast, contNonLast, contLast,
		)

		// No replacement needed when the source was already a single
		// BasicLit and the body fits in one chunk.
		if !isConcat && len(newChunks) == 1 {
			return true
		}

		var repl dst.Expr
		if len(newChunks) == 1 {
			repl = &dst.BasicLit{
				Kind:  token.STRING,
				Value: `"` + newChunks[0] + `"`,
			}
		} else {
			repl = buildPlusChain(newChunks)
		}
		copyOuterDecs(node, repl)
		c.Replace(repl)
		return false
	}, nil)
	return nil
}

// isStringExpr reports whether e is either an interpreted-string BasicLit or a
// "+" BinaryExpr whose operands are themselves string expressions
// (recursively). Raw-string literals (backticks) are excluded — splitting
// them would change the string's value.
func isStringExpr(e dst.Expr) bool {
	switch x := e.(type) {
	case *dst.BasicLit:
		if x.Kind != token.STRING {
			return false
		}
		v := x.Value
		return len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"'

	case *dst.BinaryExpr:
		if x.Op != token.ADD {
			return false
		}
		return isStringExpr(x.X) && isStringExpr(x.Y)
	}
	return false
}

// collectStringChunks walks a string-expr tree depth-first (left to right) and
// appends each leaf string body (without surrounding quotes) to chunks. Returns
// false if it encounters any node that isn't either a "+" BinaryExpr or an
// interpreted-string BasicLit — in that case the expression isn't
// pure-string-concat and we leave it alone.
func collectStringChunks(e dst.Expr, chunks *[]string) bool {
	switch x := e.(type) {
	case *dst.BasicLit:
		if x.Kind != token.STRING {
			return false
		}
		v := x.Value
		if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
			return false
		}
		*chunks = append(*chunks, v[1:len(v)-1])
		return true

	case *dst.BinaryExpr:
		if x.Op != token.ADD {
			return false
		}
		if !collectStringChunks(x.X, chunks) {
			return false
		}
		return collectStringChunks(x.Y, chunks)
	}
	return false
}

// leftmostStringLit returns the leftmost BasicLit in a string-expr tree. Its
// source position is the "first chunk" anchor for layout budgeting — the
// column at which the rewritten chain starts.
func leftmostStringLit(e dst.Expr) *dst.BasicLit {
	switch x := e.(type) {
	case *dst.BasicLit:
		return x

	case *dst.BinaryExpr:
		return leftmostStringLit(x.X)
	}
	return nil
}

// sourcePosOfLit looks up the original AST literal corresponding to a dst
// BasicLit and returns (quoteCol, lineIndent) in visual columns.
func sourcePosOfLit(ctx *Context, lit *dst.BasicLit,
	tab int) (quoteCol, lineIndent int) {

	astN, ok := ctx.Decorator.Ast.Nodes[lit]
	if !ok {
		return 0, 0
	}
	astLit, ok := astN.(*ast.BasicLit)
	if !ok {
		return 0, 0
	}
	quoteCol = visualCol(ctx.FileSet, ctx.SourceLines, astLit.Pos(), tab)
	lineIndent = lineIndentAt(
		ctx.FileSet, ctx.SourceLines, astLit.Pos(), tab,
	)
	return
}

// insideFormattingCall walks node's parent chain for a CallExpr whose callee is
// in the formatting-funcs allowlist. R5 owns those layouts.
func insideFormattingCall(ctx *Context, node dst.Node,
	parents map[dst.Node]dst.Node, allow, deny []string) bool {

	cur := node
	for {
		parent, ok := parents[cur]
		if !ok || parent == nil {
			return false
		}
		if call, isCall := parent.(*dst.CallExpr); isCall {
			astN, ok := ctx.Decorator.Ast.Nodes[call]
			if ok {
				if astCall, ok := astN.(*ast.CallExpr); ok {
					name := calleeName(astCall)
					if !inStringSetExact(name, deny) &&
						inStringSet(name, allow) {

						return true
					}
				}
			}
		}
		cur = parent
	}
}

// copyOuterDecs preserves the Before/After/Start/End decorations of the
// original node on the replacement. Without this, R4's NewLine marker on the
// original arg would be lost and the arg would collapse back inline.
func copyOuterDecs(from, to dst.Node) {
	fromDecs := from.Decorations()
	toDecs := to.Decorations()
	if fromDecs == nil || toDecs == nil {
		return
	}
	toDecs.Before = fromDecs.Before
	toDecs.After = fromDecs.After
	toDecs.Start = fromDecs.Start
	toDecs.End = fromDecs.End
}

// multiSplit greedily splits body into chunks sized for each line. firstNonLast
// and firstLast are budgets for the first chunk (with a "+" trailer vs a
// closing trailer); contNonLast and contLast are the same for continuation
// chunks. The split prefers the rightmost space within the budget; for strings
// without internal spaces it falls back to splitting at exactly the budget.
func multiSplit(body string,
	firstNonLast, firstLast, contNonLast, contLast int) []string {

	var chunks []string
	rest := body
	first := true
	for {
		lastBudget := contLast
		if first {
			lastBudget = firstLast
		}
		if len(rest) <= lastBudget {
			chunks = append(chunks, rest)
			return chunks
		}
		budget := contNonLast
		if first {
			budget = firstNonLast
		}
		at := findStringSplit(rest, budget)
		if at <= 0 || at >= len(rest) {
			chunks = append(chunks, rest)
			return chunks
		}
		chunks = append(chunks, rest[:at])
		rest = rest[at:]
		first = false
	}
}

// buildPlusChain returns a left-associated `"+"` BinaryExpr chain over the
// chunks. Every chunk after the first carries Decs.Before = dst.NewLine so
// dst's printer breaks the chain across lines.
func buildPlusChain(chunks []string) dst.Expr {
	if len(chunks) == 0 {
		return nil
	}
	if len(chunks) == 1 {
		return &dst.BasicLit{
			Kind:  token.STRING,
			Value: `"` + chunks[0] + `"`,
		}
	}
	expr := dst.Expr(&dst.BasicLit{
		Kind:  token.STRING,
		Value: `"` + chunks[0] + `"`,
	})
	for i := 1; i < len(chunks); i++ {
		right := &dst.BasicLit{
			Kind:  token.STRING,
			Value: `"` + chunks[i] + `"`,
		}
		right.Decs.Before = dst.NewLine
		expr = &dst.BinaryExpr{X: expr, Op: token.ADD, Y: right}
	}
	return expr
}

// countWrappedAncestors walks parent links and counts enclosing layout
// breaks that move the string off its source line. Two ancestor shapes
// contribute one tab each:
//
//   - CallExprs laid out as multi-line (args[0] carries a NewLine
//     before-decoration — R4's pack-form mark).
//   - BinaryExprs that R16 has operator-split (some descendant carries
//     a NewLine/EmptyLine before-decoration). The string then lives on
//     a continuation line of the broken chain, not at its source
//     column.
//
// The count converts a source-column quote position into a render-column
// one by assuming each wrap pushes the content one tab deeper, which
// matches gofmt's continuation-indent rule.
func countWrappedAncestors(node dst.Node, parents map[dst.Node]dst.Node) int {
	count := 0
	cur := node
	for {
		parent, ok := parents[cur]
		if !ok || parent == nil {
			break
		}
		switch p := parent.(type) {
		case *dst.CallExpr:
			if isCallWrapped(p) {
				count++
			}

		case *dst.BinaryExpr:
			if subtreeHasNewLineDec(p) {
				count++
			}
		}
		cur = parent
	}
	return count
}

// inMultilineContainer reports whether any ancestor of node is a container —
// slice / struct composite literal or a wrapped CallExpr — that has already
// chosen a multi-line layout (one of its elements/args carries a
// NewLine/EmptyLine Before decoration). In such a container, placing one more
// element on its own line is natural; mid-word string splitting would be the
// wrong tradeoff.
func inMultilineContainer(node dst.Node, parents map[dst.Node]dst.Node) bool {
	cur := node
	for {
		parent, ok := parents[cur]
		if !ok || parent == nil {
			return false
		}
		switch p := parent.(type) {
		case *dst.CompositeLit:
			for _, e := range p.Elts {
				d := e.Decorations()
				if d == nil {
					continue
				}
				if d.Before == dst.NewLine ||
					d.Before == dst.EmptyLine {

					return true
				}
			}

		case *dst.CallExpr:
			if isCallWrapped(p) {
				return true
			}
		}
		cur = parent
	}
}

func isCallWrapped(call *dst.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	decs := call.Args[0].Decorations()
	if decs == nil {
		return false
	}
	return decs.Before == dst.NewLine || decs.Before == dst.EmptyLine
}

// buildDstParents walks the file tree once and records every node's parent.
func buildDstParents(f *dst.File) map[dst.Node]dst.Node {
	parents := map[dst.Node]dst.Node{}
	var stack []dst.Node
	dst.Inspect(f, func(n dst.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return parents
}
