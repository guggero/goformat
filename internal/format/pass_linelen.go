package format

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/diag"
)

// splitLines, visualWidth are defined in util.go and shared with wrap passes
// that consult source-line widths.

// checkLineLength is R10: a post-render warn-only check. It does not modify the
// output; it surfaces lines that escape every wrapping pass.
//
// Exemptions implemented here:
//   - `//nolint:ll` anywhere on the line
//   - the line falls inside a nolintRange — the inclusive [start, end] line
//     span of a FuncDecl whose doc comment carried a `//nolint` directive.
//     Function-level nolint disables every check inside the function, not
//     just per-line `//nolint:ll`.
//
// Future exemptions (raw strings, URLs in comments, structured-log key-value
// sections) attach in later phases when the rules that need them land.
func checkLineLength(filename string, src []byte,
	cfg *config.Config, nolintRanges []lineRange) []diag.Diagnostic {

	if cfg.LineLength <= 0 {
		return nil
	}
	tab := cfg.TabWidth
	if tab <= 0 {
		tab = 8
	}

	var diags []diag.Diagnostic
	lineNo := 1
	for _, line := range splitLines(src) {
		width := visualWidth(line, tab)
		if width > cfg.LineLength && !isExempt(line) &&
			!inAnyRange(lineNo, nolintRanges) {

			diags = append(diags, diag.Diagnostic{
				Rule:     "R10",
				Severity: diag.Warn,
				File:     filename,
				Line:     lineNo,
				Col:      cfg.LineLength + 1,
				Message: fmt.Sprintf(
					"line exceeds %d cols (got %d)",
					cfg.LineLength, width,
				),
			})
		}
		lineNo++
	}
	return diags
}

// inAnyRange reports whether lineNo falls within any of the inclusive
// 1-based ranges.
func inAnyRange(lineNo int, ranges []lineRange) bool {
	for _, r := range ranges {
		if lineNo >= r.start && lineNo <= r.end {
			return true
		}
	}
	return false
}

func isExempt(line []byte) bool {
	return bytes.Contains(line, []byte("//nolint:ll")) ||
		bytes.Contains(line, []byte("// nolint:ll")) ||
		strings.Contains(string(line), "//nolint:lll")
}
