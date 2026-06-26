package format

import (
	"strings"
	"unicode/utf8"

	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// commentReflow implements R15: single-line "//"-style comments that exceed the
// limit get reflowed to multiple lines at word boundaries. Block comments (/*
// … */), tool directives (//go:…, //nolint:…) and already-short lines are
// left alone.
//
// Scope: dst node Start and End decoration slots. Other comment slots
// (CallExpr's Lparen/Rparen, BinaryExpr's Op, …) are rare for prose comments
// and stay untouched in v1.
type commentReflow struct{}

func (commentReflow) Name() string { return "R15" }

func (commentReflow) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.CommentReflowOn() {
		return nil
	}
	tab := ctx.Config.TabWidth
	if tab <= 0 {
		tab = 8
	}
	limit := ctx.Config.LineLength
	if limit <= 0 {
		limit = 80
	}

	dst.Inspect(ctx.File, func(n dst.Node) bool {
		if ctx.SkipNolintDecl(n) {
			return false
		}
		if n == nil {
			return true
		}
		decs := n.Decorations()
		if decs == nil {
			return true
		}

		// Indent: visual column where the comment will be rendered. dst
		// aligns Start/End comments with the containing node's
		// source-line indent.
		indent := 0
		if astN, ok := ctx.Decorator.Ast.Nodes[n]; ok && astN != nil {
			indent = lineIndentAt(
				ctx.FileSet, ctx.SourceLines, astN.Pos(), tab,
			)
		}

		decs.Start = reflowCommentBlock(decs.Start, indent, limit)
		decs.End = reflowCommentBlock(decs.End, indent, limit)
		return true
	})
	return nil
}

// reflowCommentBlock groups adjacent prose-comment lines into a single logical
// paragraph, then re-splits the joined text at word boundaries so each chunk
// fits the budget. This is what fixes the "tiny tail" case — source line `//
// ... outputs are no` ends with one short word that, when processed in
// isolation, gets split into its own one-word line. Joining the whole paragraph
// first and re-flowing as one body avoids that.
//
// Paragraph breaks (the joined paragraph is flushed and the breaker is emitted
// verbatim):
//   - tool directives (//go: , //nolint:, //line)
//   - block comments (/* ... */)
//   - empty `//` lines
//   - indented continuation lines (body starts with extra spaces) —
//     these typically denote example code or a continued list item
//     and shouldn't be reflowed as prose
//   - list-marker lines (`- `, `* `, `N. `)
//   - non-comment entries
func reflowCommentBlock(in []string, indent, limit int) []string {
	if len(in) == 0 {
		return in
	}
	budget := limit - indent - len("// ")
	if budget < 10 {
		return in
	}
	out := make([]string, 0, len(in))
	var paragraph []string

	flush := func() {
		if len(paragraph) == 0 {
			return
		}

		// Reflow ONLY when at least one paragraph line is over the
		// budget. Re-flowing fitting prose just to match a wider
		// indent churns diffs and overrides the developer's chosen
		// line breaks — the doc treats line-length as a ceiling,
		// not a target.
		needsReflow := false
		for _, body := range paragraph {
			if utf8.RuneCountInString(body) > budget {
				needsReflow = true
				break
			}
		}
		if !needsReflow {
			for _, body := range paragraph {
				out = append(out, "// "+body)
			}
			paragraph = paragraph[:0]
			return
		}
		joined := strings.Join(paragraph, " ")
		if utf8.RuneCountInString(joined) <= budget {
			out = append(out, "// "+joined)
			paragraph = paragraph[:0]
			return
		}
		for _, ch := range splitCommentBody(joined, budget) {
			out = append(out, "// "+ch)
		}
		paragraph = paragraph[:0]
	}

	outputMode := false
	for _, entry := range in {
		// A Go example test's expected-output block ("// Output:" /
		// "// Unordered output:") is compared verbatim by the testing
		// framework. Once it starts, emit it and everything after it in
		// this comment block unchanged — reflowing it breaks the test.
		if outputMode {
			out = append(out, entry)
			continue
		}
		if isOutputDirective(entry) {
			flush()
			outputMode = true
			out = append(out, entry)
			continue
		}
		if isParagraphBreaker(entry) {
			flush()
			out = append(out, entry)
			continue
		}
		paragraph = append(paragraph, commentBody(entry))
	}
	flush()
	return out
}

// isOutputDirective reports whether a "//" comment line begins a Go example
// test's expected-output block. Matched case-insensitively, mirroring
// go/doc's example detection ("// Output:" / "// Unordered output:").
func isOutputDirective(entry string) bool {
	body := strings.ToLower(
		strings.TrimSpace(strings.TrimPrefix(entry, "//")),
	)
	return strings.HasPrefix(body, "output:") ||
		strings.HasPrefix(body, "unordered output:")
}

// isParagraphBreaker reports whether an entry should end the current paragraph
// and be emitted as-is (rather than merged into the running prose body for
// reflow).
func isParagraphBreaker(entry string) bool {
	if !strings.HasPrefix(entry, "//") {
		return true
	}
	if strings.HasPrefix(entry, "//go:") ||
		strings.HasPrefix(entry, "//nolint") ||
		strings.HasPrefix(entry, "// nolint") ||
		strings.HasPrefix(entry, "//line ") {

		return true
	}
	if strings.HasPrefix(entry, "/*") {
		return true
	}
	body := strings.TrimPrefix(entry, "//")

	// Empty "//" line — paragraph separator.
	if strings.TrimSpace(body) == "" {
		return true
	}

	// "///"-style banner/divider comments (body begins with another "/")
	// are not prose — reflowing/normalizing them would turn "///" into
	// "// /". Leave them verbatim.
	if strings.HasPrefix(body, "/") {
		return true
	}

	// Indented continuation (extra leading spaces OR a leading tab) — code,
	// an ASCII diagram, or a list continuation. These are preformatted, so
	// leave the line exactly as-is. Tab-indented comment bodies ("//\t…",
	// common for lnd's storage-hierarchy diagrams) must be matched here
	// too, otherwise they get normalized to "// \t…" and the art shifts.
	if strings.HasPrefix(body, "  ") || strings.HasPrefix(body, "\t") {
		return true
	}

	// Strip the one canonical space and inspect the head for list markers.
	body = strings.TrimPrefix(body, " ")
	return isListMarker(body)
}

// isListMarker reports whether body starts with a list marker ("- ", "* ", "+
// ", or "N. " / "N) " for a one- or two-digit N).
func isListMarker(body string) bool {
	if len(body) < 2 {
		return false
	}
	switch body[0] {
	case '-', '*', '+':
		return body[1] == ' '
	}
	i := 0
	for i < len(body) && body[i] >= '0' && body[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(body) {
		return false
	}
	if body[i] != '.' && body[i] != ')' {
		return false
	}
	return body[i+1] == ' '
}

// commentBody strips the leading "//" and at most one following space.
// "//hello" and "// hello" both produce "hello".
func commentBody(entry string) string {
	body := strings.TrimPrefix(entry, "//")
	return strings.TrimPrefix(body, " ")
}

// splitCommentBody splits text into chunks each ≤ budget chars, preferring
// the rightmost space within the budget. A run with no space within the budget
// (a long identifier or URL) ends up alone on its own chunk — better than
// mid-word breakage.
func splitCommentBody(text string, budget int) []string {
	// Operate on runes, not bytes: budgets are column counts, and indexing
	// a byte slice would both miscount multi-byte runes (em-dash, …) and
	// risk splitting one mid-rune.
	rest := []rune(text)
	if budget <= 0 || len(rest) <= budget {
		return []string{text}
	}
	var chunks []string
	for len(rest) > budget {
		splitAt := -1
		for i := budget; i > 0; i-- {
			if rest[i] == ' ' {
				splitAt = i
				break
			}
		}
		if splitAt <= 0 {
			// No space within budget — find the next space (so
			// the long token sits on its own line) or give up if
			// there is none.
			for i := budget; i < len(rest); i++ {
				if rest[i] == ' ' {
					splitAt = i
					break
				}
			}
		}
		if splitAt <= 0 {
			break
		}
		chunks = append(chunks, string(rest[:splitAt]))
		j := splitAt
		for j < len(rest) && rest[j] == ' ' {
			j++
		}
		rest = rest[j:]
	}
	if len(rest) > 0 {
		chunks = append(chunks, string(rest))
	}
	return chunks
}
