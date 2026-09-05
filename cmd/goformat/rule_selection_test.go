package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuleSelectionOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "goformat.toml")
	if err := os.WriteFile(
		configPath, []byte(
			`[rules]
switch_case_spacing = false
stanza_spacing = false
declaration_grouping = true
`,
		), 0o644,
	); err != nil {

		t.Fatal(err)
	}
	src := `package p

func f(n int) {
	switch n {
	case 1:
		println(1)
	case 2:
		println(2)
	}
	println(n)
	// Next step.
	println(n)
}

var value int
`
	want := strings.Replace(src, "\tcase 2:", "\n\tcase 2:", 1)
	want = strings.Replace(want, "\t// Next step.", "\n\t// Next step.", 1)
	for _, flags := range [][]string{
		{"--rule", "R1,R11"},
		{"--rule", "R1", "--rule", "r11,R1"},
	} {

		args := append([]string{"--config", configPath}, flags...)
		var out, diagnostics bytes.Buffer
		err := run(
			append(args, "-w"), strings.NewReader(src), &out,
			&diagnostics,
		)
		if err != nil || out.String() != want ||
			diagnostics.Len() != 0 {

			t.Fatalf("%v: %v\n%s\n%s", flags, err, out.String(), diagnostics.String())
		}
		out.Reset()
		err = run(
			append(args, "-check"), strings.NewReader(src), &out,
			&diagnostics,
		)
		if !errors.Is(err, errChangesNeeded) {
			t.Fatalf("check: %v", err)
		}
		err = run(
			append(args, "-check"), strings.NewReader(want), &out,
			&diagnostics,
		)
		if err != nil {
			t.Fatalf("check selected rules after fixing: %v", err)
		}
	}
}

func TestRuleSelectionInvalid(t *testing.T) {
	for _, value := range []string{
		"", "R0", "R17", "R1,", "R1,typo", "no" +
			"format",
	} {

		t.Run(value, func(t *testing.T) {
			_, _, err := runForTest(t, "--rule", value, "-w")
			if err == nil ||
				!strings.Contains(err.Error(), "rule") {

				t.Fatalf("expected rule error, got %v", err)
			}
		})
	}
}

func TestRuleSelectionLineLength(t *testing.T) {
	src := "package p\n\nvar   x = `" + strings.Repeat("x", 90) + "`\n"
	var out, diagnostics bytes.Buffer
	err := run(
		[]string{"--no-config", "--rule", "R10", "-check"},
		strings.NewReader(src), &out, &diagnostics,
	)
	if !errors.Is(err, errChangesNeeded) ||
		!strings.Contains(diagnostics.String(), "[R10]") {

		t.Fatalf("check: %v %s", err, diagnostics.String())
	}
	diagnostics.Reset()
	err = run(
		[]string{"--no-config", "--rule", "R10", "-w"},
		strings.NewReader(src), &out, &diagnostics,
	)
	if err != nil || out.String() != src ||
		!strings.Contains(diagnostics.String(), "[R10]") {

		t.Fatalf("warn-only write: %v\n%s\n%s", err, out.String(), diagnostics.String())
	}
}

func TestRuleSelectionGit(t *testing.T) {
	r := newTestRepo(t)
	baseline := `package p

func f(n int) {
	switch n {
	case 1:
		println(1)
	case 2:
		println(2)
	}
	println(n)
	// Next step.
	println(n)
}
`
	path := r.write("p.go", baseline)
	r.commit()
	r.write("p.go", strings.Replace(
		baseline, "Next step.", "Changed step.", 1,
	))
	_, _, err := runForTest(
		t, "--rule", "R11", "--unstaged-only", "-w", path,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(
		baseline, "\t// Next step.", "\n\t// Changed step.", 1,
	)
	if got := r.read("p.go"); got != want {
		t.Fatalf("unexpected scoped formatting:\n%s", got)
	}
}

func TestRuleSelectionGitDiagnostics(t *testing.T) {
	r := newTestRepo(t)
	baseline := "package p\n\nvar untouched = `" + strings.Repeat("x", 90) +
		"`\n\nvar changed = `short`\n"
	path := r.write("p.go", baseline)
	r.commit()
	r.write("p.go", strings.Replace(
		baseline, "short", strings.Repeat("y", 90), 1,
	))
	_, diagnostics, err := runForTest(
		t, "--rule", "R10", "--unstaged-only", "-check", path,
	)
	if !errors.Is(err, errChangesNeeded) ||
		strings.Count(diagnostics, "[R10]") != 1 ||
		!strings.Contains(diagnostics, ":5:") {

		t.Fatalf("scoped diagnostics: %v %s", err, diagnostics)
	}
}

func TestRuleSelectionGitDiagnosticPositions(t *testing.T) {
	r := newTestRepo(t)
	baseline := `package p

func untouched() {
	ordinary(firstLongArgument, secondLongArgument, thirdLongArgument, fourthLongArgument)
}

var changed = ` +
		"`short`\n"
	path := r.write("p.go", baseline)
	r.commit()
	r.write("p.go", strings.Replace(
		baseline, "short", strings.Repeat("y", 90), 1,
	))
	_, diagnostics, err := runForTest(
		t, "--rule", "R4,R10", "--unstaged-only", "-check", path,
	)
	if !errors.Is(err, errChangesNeeded) ||
		strings.Count(diagnostics, "[R10]") != 1 ||
		!strings.Contains(diagnostics, ":7:") {

		t.Fatalf("scoped diagnostic positions: %v %s", err, diagnostics)
	}
}

func TestRuleSelectionFileModes(t *testing.T) {
	r := newTestRepo(t)
	src := `package p

func f() {
	println(1)
	// Next step.
	println(2)
}

var value int
`
	path := r.write("p.go", src)
	for _, mode := range []string{"-l", "-d", "-check"} {
		out, _, err := runForTest(t, "--rule", "R11", mode, path)
		if mode == "-check" {
			if !errors.Is(err, errChangesNeeded) {
				t.Fatalf("check: %v", err)
			}
		} else if err != nil || !strings.Contains(out, path) {
			t.Fatalf("%s: %v %s", mode, err, out)
		}
		if got := r.read("p.go"); got != src {
			t.Fatalf("%s wrote the file", mode)
		}
	}
	if _, _, err := runForTest(t, "--rule", "R11", "-w", path); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, "\t// Next step.", "\n\t// Next step.", 1)
	if got := r.read("p.go"); got != want {
		t.Fatalf("unexpected written contents:\n%s", got)
	}
}
