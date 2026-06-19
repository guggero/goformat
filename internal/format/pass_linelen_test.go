package format

import (
	"strings"
	"testing"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/diag"
)

func filterRule(diags []diag.Diagnostic, rule string) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range diags {
		if d.Rule == rule {
			out = append(out, d)
		}
	}
	return out
}

func TestR10_LongLineWarns(t *testing.T) {
	// A 100-column string literal blows past the 80-col default and should
	// surface as exactly one R10 diagnostic. Disable R9 so it doesn't
	// preemptively split the literal we want to detect as long.
	src := []byte(
		`package x

var Long = "` + strings.Repeat("x", 100) + `"
`,
	)
	cfg := config.Default()
	off := false
	cfg.Rules.StringLitWrap = &off
	_, diags, err := Format(src, "x.go", cfg)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	r10 := filterRule(diags, "R10")
	if len(r10) != 1 {
		t.Fatalf("want 1 R10 diagnostic, got %d (all diags: %v)",
			len(r10), diags)
	}
	if r10[0].Line != 3 {
		t.Errorf("R10 on line %d, want 3", r10[0].Line)
	}
}

func TestR10_NolintExempt(t *testing.T) {
	src := []byte(
		`package x

var Long = "` + strings.Repeat("x", 100) + `" //nolint:ll
`,
	)
	cfg := config.Default()

	// Force R9 off — otherwise it splits the literal before R10 runs and
	// the line-with-nolint:ll never appears in the output.
	off := false
	cfg.Rules.StringLitWrap = &off
	_, diags, err := Format(src, "x.go", cfg)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if got := len(filterRule(diags, "R10")); got != 0 {
		t.Errorf("nolint:ll line should be exempt, got %d R10 diags",
			got)
	}
}

func TestR10_TabsCountAs8Cols(t *testing.T) {
	// Two tabs + 72 chars = 16 + 72 = 88 visual cols. Exceeds 80. Wrap in a
	// raw string so gofmt doesn't try to break our backticks.
	src := []byte(
		"package x\n\nvar Long = `\n\t\t" +
			strings.Repeat("x", 72) + "`\n",
	)
	_, diags, err := Format(src, "x.go", config.Default())
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if got := len(filterRule(diags, "R10")); got == 0 {
		t.Errorf("expected R10 on tab-padded long line, got none")
	}
}

func TestR10_RespectsCustomLength(t *testing.T) {
	src := []byte(
		`package x

var V = "` + strings.Repeat("x", 50) + `"
`,
	)
	cfg := config.Default()
	cfg.LineLength = 40
	off := false
	cfg.Rules.StringLitWrap = &off
	_, diags, err := Format(src, "x.go", cfg)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if got := len(filterRule(diags, "R10")); got != 1 {
		t.Errorf("want 1 R10 at custom length 40, got %d", got)
	}
}
