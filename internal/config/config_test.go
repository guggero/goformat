package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsLocked(t *testing.T) {
	c := Default()
	if c.LineLength != 80 {
		t.Errorf("line_length default = %d, want 80", c.LineLength)
	}
	if c.TabWidth != 8 {
		t.Errorf("tab_width default = %d, want 8", c.TabWidth)
	}
	if c.Canonicalize {
		t.Error("canonicalize default = true, want false")
	}
	if !c.Rules.SwitchCaseSpacingOn() {
		t.Error("switch_case_spacing should default on")
	}
	if !c.Rules.FuncCallWrapOn() {
		t.Error("func_call_wrap should default on")
	}
}

func TestLoadMissingIsDefault(t *testing.T) {
	c, err := Load("/does/not/exist/goformat.toml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.LineLength != 80 {
		t.Errorf("missing file should yield defaults, got "+
			"LineLength=%d", c.LineLength)
	}
}

func TestLoadOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goformat.toml")
	body := []byte(
		`
line_length = 100
tab_width = 4
canonicalize = true
formatting_funcs = ["fmt.Errorf"]

[rules]
func_call_wrap = false
`,
	)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.LineLength != 100 {
		t.Errorf("LineLength = %d, want 100", c.LineLength)
	}
	if c.TabWidth != 4 {
		t.Errorf("TabWidth = %d, want 4", c.TabWidth)
	}
	if !c.Canonicalize {
		t.Error("canonicalize override lost")
	}
	if len(c.FormattingFuncs) != 1 || c.FormattingFuncs[0] != "fmt.Errorf" {
		t.Errorf("FormattingFuncs override lost: %v", c.FormattingFuncs)
	}
	if c.Rules.FuncCallWrapOn() {
		t.Error("func_call_wrap=false override lost")
	}

	// Unset rules should still default on.
	if !c.Rules.SwitchCaseSpacingOn() {
		t.Error("unset rule should default on")
	}
}

func TestFindWalksUp(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, "goformat.toml")
	if err := os.WriteFile(cfgPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Find(deep)
	want, _ := filepath.Abs(cfgPath)
	if got != want {
		t.Errorf("Find = %q, want %q", got, want)
	}
}

func TestFindNotFound(t *testing.T) {
	root := t.TempDir()
	if got := Find(root); got != "" {
		t.Errorf("Find with no config = %q, want empty", got)
	}
}
