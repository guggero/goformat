package format

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guggero/goformat/internal/config"
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

	cfg := config.Default()
	for _, in := range matches {
		in := in
		base := strings.TrimSuffix(filepath.Base(in), ".in.go")
		rule := filepath.Base(filepath.Dir(in))
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
	cfg := config.Default()
	for _, out := range matches {
		out := out
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

// TestParseError surfaces parse failures as errors (not silent passes).
func TestParseError(t *testing.T) {
	_, _, err := Format(
		[]byte("package x\nfunc {"), "x.go", config.Default(),
	)
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}
