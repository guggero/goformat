package format

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/syntax"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "regenerate .out.go golden files")

// TestUnitPairs walks testdata/unit/*/ collecting <name>.in.go files and runs
// Format on each, asserting the output matches its <name>.out.go sibling. With
// -update, .out.go files are (re)written instead.
func TestUnitPairs(t *testing.T) {
	matches, err := filepath.Glob("../../testdata/*/*.in.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("no testdata/unit pairs found")
	}

	for _, in := range matches {
		in := in
		base := strings.TrimSuffix(filepath.Base(in), ".in.go")
		rule := filepath.Base(filepath.Dir(in))
		cfg := cfgForPair(base)
		t.Run(rule+"/"+base, func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			got, _, err := Format(src, in, cfg)
			if err != nil {
				t.Fatalf("format: %v", err)
			}
			outPath := strings.TrimSuffix(in, ".in.go") + ".out.go"
			if *update {
				err := os.WriteFile(outPath, got, 0o644)
				if err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatalf("read golden %s: %v", outPath, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("formatter output differs from %s\n"+
					"--- got ---\n%s\n--- want "+
					"---\n%s", outPath, got, want)
			}
		})
	}
}

// TestReflowInvariants complements the readable call and string regression
// pairs with syntax, width, and stability checks. These fixtures only reflow
// expressions; rules that deliberately regroup declarations need other checks.
func TestReflowInvariants(t *testing.T) {
	for _, pattern := range []string{
		"../../testdata/R4_funccall/nested_partial*.in.go",
		"../../testdata/R6_symmetry/nested_partial_session*.in.go",
		"../../testdata/R6_symmetry/symmetry_line_count*.in.go",
		"../../testdata/R9_strlit/callback_indent*.in.go",
	} {

		matches, err := filepath.Glob(pattern)
		require.NoError(t, err)
		require.NotEmpty(t, matches)
		for _, in := range matches {
			t.Run(filepath.Base(in), func(t *testing.T) {
				// Use the same inputs and mode selection as the
				// golden tests so the extra assertions cannot
				// drift away from the documented examples.
				src, err := os.ReadFile(in)
				require.NoError(t, err)
				base := strings.TrimSuffix(
					filepath.Base(in), ".in.go",
				)
				cfg := cfgForPair(base)
				out, diagnostics, err := Format(src, in, cfg)
				require.NoError(t, err)
				require.Empty(t, diagnostics)

				// A visually correct result must also preserve
				// the program and reach a fixed point. Literal
				// concatenations are compared by string value.
				before, err := syntax.Fingerprint(src)
				require.NoError(t, err)
				after, err := syntax.Fingerprint(out)
				require.NoError(t, err)
				require.Equal(t, string(before), string(after))
				again, _, err := Format(out, in, cfg)
				require.NoError(t, err)
				require.Equal(t, string(out), string(again))
			})
		}
	}
}

// TestIdempotent asserts every .out.go reaches a fixed point: running Format on
// it twice produces zero changes.
func TestIdempotent(t *testing.T) {
	matches, err := filepath.Glob("../../testdata/*/*.out.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("no testdata/unit golden files found")
	}
	for _, out := range matches {
		out := out
		base := strings.TrimSuffix(filepath.Base(out), ".out.go")
		cfg := cfgForPair(base)
		t.Run(filepath.Base(out), func(t *testing.T) {
			src, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			once, _, err := Format(src, out, cfg)
			if err != nil {
				t.Fatalf("format pass 1: %v", err)
			}
			twice, _, err := Format(once, out, cfg)
			if err != nil {
				t.Fatalf("format pass 2: %v", err)
			}
			if !bytes.Equal(once, twice) {
				t.Errorf("non-idempotent on %s", out)
			}
		})
	}
}

// cfgForPair returns the config a testdata pair is formatted with. A pair whose
// base name ends in ".opt" (e.g. collapse.opt.in.go) is formatted with
// Optimize=true so we can keep coverage of the soft, space-efficiency layouts
// (collapse, symmetry-on-fitting-code, string joins) that are off by default.
func cfgForPair(base string) *config.Config {
	cfg := config.Default()
	if strings.HasSuffix(base, ".opt") {
		cfg.Optimize = true
	}
	return cfg
}

// TestParseError surfaces parse failures as errors (not silent passes).
func TestParseError(t *testing.T) {
	_, _, err := Format(
		[]byte("package x\nfunc {"), "x.go", config.Default(),
	)
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}
