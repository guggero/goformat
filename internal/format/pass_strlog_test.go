package format

import (
	"testing"

	"github.com/guggero/goformat/internal/config"
)

func TestR8_NonStaticMsgLints(t *testing.T) {
	src := []byte(
		`package x

import "fmt"

func f(ctx C, n int) {
	log.InfoS(ctx, fmt.Sprintf("dynamic %d", n))
}

type C struct{}

var log = struct {
	InfoS func(ctx C, msg string, args ...any)
}{}
`,
	)
	_, diags, err := Format(src, "x.go", config.Default())
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	got := filterRule(diags, "R8")
	if len(got) != 1 {
		t.Fatalf("want 1 R8 diagnostic, got %d (all: %v)", len(got),
			diags)
	}
	if got[0].Line != 6 {
		t.Errorf("R8 on line %d, want 6", got[0].Line)
	}
}

func TestR8_StaticMsgClean(t *testing.T) {
	src := []byte(
		`package x

func f(ctx C, n int) {
	log.InfoS(ctx, "user connected")
}

type C struct{}

var log = struct {
	InfoS func(ctx C, msg string, args ...any)
}{}
`,
	)
	_, diags, err := Format(src, "x.go", config.Default())
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if got := filterRule(diags, "R8"); len(got) != 0 {
		t.Errorf("want 0 R8 diagnostics for static msg, got %d",
			len(got))
	}
}

func TestR8_NonLogCallSkipped(t *testing.T) {
	// A method ending in S that ISN'T a log call (no ctx first arg).
	src := []byte(
		`package x

func f() {
	type T struct{}
	var t T
	_ = t.MarshalAs(42)
}

type T struct{}

func (T) MarshalAs(int) string { return "" }
`,
	)
	_, diags, err := Format(src, "x.go", config.Default())
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if got := filterRule(diags, "R8"); len(got) != 0 {
		t.Errorf("MarshalAs shouldn't trip R8, got %d", len(got))
	}
}
