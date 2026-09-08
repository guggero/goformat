package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// enterVerificationRepo restores the caller's directory after a CLI test. These
// tests cannot run in parallel because the CLI resolves Git from the process
// cwd.
func enterVerificationRepo(t *testing.T, dir string) {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(cwd))
	})
}

// runVerification exercises the public flag parser without the formatting-only
// flags normally supplied by runForTest, and captures both output streams.
func runVerification(args ...string) (string, string, error) {
	var out, diagnostics bytes.Buffer
	err := run(args, strings.NewReader(""), &out, &diagnostics)
	return out.String(), diagnostics.String(), err
}

// TestVerifyDiffFormatting checks both endpoint orders over three commits and
// proves that dirty files, the index, and formatter exclusions do not affect
// it.
func TestVerifyDiffFormatting(t *testing.T) {
	r := newTestRepo(t)
	r.write(
		"testdata/x.go",
		"package p\n// A longer comment.\nfunc f() { call(\"hello "+
			"world\") }\n",
	)
	r.write("goformat.toml", "this is not valid TOML [")
	r.commit()

	// Distribute the same syntax across three commits so an implementation
	// comparing only the last parent, or using rev-list backwards, is
	// exposed.
	r.write(
		"testdata/x.go",
		"package p\n// A longer comment.\nfunc f() {\n call(\"hello "+
			"world\")\n}\n",
	)
	r.commit()
	r.write(
		"testdata/x.go",
		"package p\n// A longer\n// comment.\nfunc f() {\n "+
			"call(\"hello world\")\n}\n",
	)
	r.commit()
	r.write(
		"testdata/x.go",
		"package p\n// A longer\n// comment.\nfunc f() {\n "+
			"call(\"hello \" +\n \"world\",)\n}\n",
	)
	r.commit()
	r.write("testdata/x.go", "invalid staged source")
	r.git("add", "testdata/x.go")
	r.write("testdata/x.go", "different invalid working source")
	before := r.git("status", "--porcelain=v1")
	index := r.git("show", ":testdata/x.go")
	enterVerificationRepo(t, filepath.Join(r.root, "testdata"))

	for _, revisions := range []string{"HEAD~3..HEAD", "HEAD..HEAD~3"} {
		out, diagnostics, err := runVerification(
			"-verify-diff", revisions,
		)
		require.NoError(t, err)
		require.Empty(t, out)
		require.Empty(t, diagnostics)
	}
	require.Equal(t, before, r.git("status", "--porcelain=v1"))
	require.Equal(t, index, r.git("show", ":testdata/x.go"))
	require.Equal(t, "different invalid working source", r.read(
		"testdata/x.go",
	))
}

// TestVerifyDiffMeaningfulChanges verifies the shared syntax checker's AST
// invariants through the actual commit-range CLI.
func TestVerifyDiffMeaningfulChanges(t *testing.T) {
	tests := []struct {
		name, before, after, expected string
	}{
		{
			name: "number", before: "package p; var n = 1",
			after: "package p; var n = 2", expected: `"Value": "2"`,
		},
		{
			name: "string space", before: `package p; var s = "a b"`,
			after: `package p; var s = "ab"`, expected: `\"ab\"`,
		},
		{
			name: "invalid UTF8", before: `package p; var s = "\xff"`,
			after: `package p; var s = "\xfe"`, expected: `\\xfe`,
		},
		{
			name: "comment", before: "package p\n// Before.\nvar " +
				"n = 1",
			after: "package p\n// After.\nvar n = 1", expected: "After.",
		},
		{
			name: "variadic", before: "package p; func f() { " +
				"call(xs...) }",
			after: "package p; func f() { call(xs) }", expected: `"Ellipsis"`,
		},
		{
			name: "alias", before: "package p; type T = U",
			after: "package p; type T U", expected: `"Assign"`,
		},
		{
			name: "directive", before: "//go:build " +
				"linux\n\npackage p",
			after: "//go:build darwin\n\npackage p", expected: "darwin",
		},
		{
			name: "cgo", before: "package p\n/* char *s = \"a " +
				"b\"; */\nimport \"C\"",
			after: "package p\n/* char *s = \"a  b\"; */\nimport " +
				"\"C\"", expected: "a  b",
		},
		{
			name: "declaration collection", before: "package p; " +
				"var a = 1; var b = 2",
			after: "package p; var (a = 1; b = 2)", expected: "@@",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRepo(t)
			r.write("x.go", tc.before)
			r.commit()
			r.write("x.go", tc.after)
			r.commit()
			enterVerificationRepo(t, r.root)

			out, diagnostics, err := runVerification(
				"-verify-diff", "HEAD^..HEAD",
			)
			require.ErrorIs(t, err, errChangesNeeded)
			require.Empty(t, diagnostics)
			require.Contains(t, out, `--- "a/x.go" (canonical AST)`)
			require.Contains(t, out, tc.expected)
		})
	}
}

// TestVerifyDiffWholeRange distinguishes snapshot comparison from per-commit
// validation, and catches meaningful changes made before the final commit.
func TestVerifyDiffWholeRange(t *testing.T) {
	r := newTestRepo(t)
	r.write("x.go", "package p; var n = 1")
	r.commit()
	r.write("x.go", "package p; var n = 2")
	r.commit()
	r.write("x.go", "package p\nvar n = 2\n")
	r.commit()
	enterVerificationRepo(t, r.root)

	for _, revisions := range []string{"HEAD~2..HEAD", "HEAD..HEAD~2"} {
		out, _, err := runVerification("-verify-diff", revisions)
		require.ErrorIs(t, err, errChangesNeeded)
		require.NotEmpty(t, out)
	}

	// Reverting the semantic change leaves a formatting-only net diff.
	// This explicitly defines the claim made by an empty successful report.
	r.write("x.go", "package p\nvar n = 1\n")
	r.commit()
	out, _, err := runVerification("-verify-diff", "HEAD~3..HEAD")
	require.NoError(t, err)
	require.Empty(t, out)
}

// TestVerifyDiffMetadata ensures paths and Git metadata cannot disappear from
// the attestation merely because they are not ordinary modified Go files.
func TestVerifyDiffMetadata(t *testing.T) {
	r := newTestRepo(t)
	for _, name := range []string{
		"mode.go", "deleted.go", "renamed.go", "link.go",
	} {

		r.write(name, "package p\n")
	}
	r.write("README.md", "before\n")
	r.commit()
	r.write("added.go", "package p\n")
	r.write("odd\nname.go", "package p\n")
	r.write("README.md", "after\n")
	r.git("update-index", "--chmod=+x", "mode.go")
	require.NoError(t, os.Chmod(filepath.Join(r.root, "mode.go"), 0o755))
	require.NoError(t, os.Remove(filepath.Join(r.root, "deleted.go")))
	require.NoError(t, os.Rename(
		filepath.Join(r.root, "renamed.go"),
		filepath.Join(r.root, "newname.go"),
	))
	require.NoError(t, os.Remove(filepath.Join(r.root, "link.go")))
	require.NoError(t, os.Symlink(
		"added.go", filepath.Join(r.root, "link.go"),
	))
	r.commit()
	enterVerificationRepo(t, r.root)

	out, _, err := runVerification("-verify-diff", "HEAD^..HEAD")
	require.ErrorIs(t, err, errChangesNeeded)
	for _, path := range []string{
		"mode.go", "deleted.go", "renamed.go", "newname.go", "link.go",
		"README.md", "added.go", `odd\nname.go`,
	} {

		require.Contains(t, out, path)
	}
}

// TestVerifyDiffErrors prevents malformed ranges, unsupported filters, and
// unparsable snapshots from looking like successful empty comparisons.
func TestVerifyDiffErrors(t *testing.T) {
	r := newTestRepo(t)
	r.write("x.go", "package p\n")
	r.commit()
	r.write("x.go", "not Go")
	r.commit()
	enterVerificationRepo(t, r.root)

	for _, revisions := range []string{
		"", "HEAD", "HEAD..", "..HEAD", "HEAD...HEAD",
		"HEAD..HEAD..HEAD", "missing..HEAD", "HEAD^{tree}..HEAD",
		"HEAD^..HEAD",
	} {

		out, _, err := runVerification("-verify-diff", revisions)
		require.Error(t, err, revisions)
		require.False(t, errors.Is(err, errChangesNeeded), revisions)
		require.Empty(t, out, revisions)
	}
	for _, extra := range [][]string{
		{
			"-w",
		}, {
			"-d",
		}, {
			"-l",
		}, {
			"-check",
		}, {
			"-rule", "R1",
		},
		{
			"-exclude", "x.go",
		}, {
			"-config", "missing",
		}, {
			"-no-config",
		},
		{
			"-uncommitted-only",
		}, {
			"-unstaged-only",
		}, {
			"-rules",
		},
		{
			"-optimize",
		}, {
			".",
		}, {
			"-",
		},
	} {

		args := append([]string{"-verify-diff", "HEAD..HEAD"}, extra...)
		out, _, err := runVerification(args...)
		require.ErrorContains(t, err, "without paths or other flags")
		require.Empty(t, out)
	}
}

// TestVerifyDiffScopeAndSubmodules checks repository-wide coverage despite
// cwd-relative and submodule-ignore settings in the user's Git configuration.
func TestVerifyDiffScopeAndSubmodules(t *testing.T) {
	r := newTestRepo(t)
	r.write("root.go", "package p; var n = 1")
	r.write("nested/unchanged.go", "package p\n")
	r.commit()
	first := strings.TrimSpace(string(r.git("rev-parse", "HEAD")))
	r.git(
		"update-index", "--add", "--cacheinfo", "160000", first,
		"submodule",
	)
	r.git("-c", "commit.gpgsign=false", "commit", "-qm", "add submodule")
	second := strings.TrimSpace(string(r.git("rev-parse", "HEAD")))

	// Neither a hidden submodule nor an out-of-directory Go change may
	// disappear from the report. Stage directly to retain the gitlink.
	r.write("root.go", "package p; var n = 2")
	r.git("add", "root.go")
	r.git("update-index", "--cacheinfo", "160000", second, "submodule")
	r.git("-c", "commit.gpgsign=false", "commit", "-qm", "update both")
	r.git("config", "diff.relative", "true")
	r.git("config", "diff.ignoreSubmodules", "all")
	enterVerificationRepo(t, filepath.Join(r.root, "nested"))
	out, _, err := runVerification("-verify-diff", "HEAD^..HEAD")
	require.ErrorIs(t, err, errChangesNeeded)
	require.Contains(t, out, "root.go")
	require.Contains(t, out, "submodule")
	require.Contains(t, out, "Subproject commit")
}

// TestVerifyDiffCompleteReport prevents the ordinary formatter's abbreviated
// diff behavior from truncating a verification report, and checks atomic
// errors.
func TestVerifyDiffCompleteReport(t *testing.T) {
	r := newTestRepo(t)
	var before, after strings.Builder
	before.WriteString("package p\n")
	after.WriteString("package p\n")
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		before.WriteString("var " + name + " = 1\n")
		after.WriteString("var " + name + " = 2\n")
	}
	r.write("a.go", before.String())
	r.write("z.go", "package p\n")
	r.commit()
	r.write("a.go", after.String())
	r.commit()
	enterVerificationRepo(t, r.root)
	out, _, err := runVerification("-verify-diff", "HEAD^..HEAD")
	require.ErrorIs(t, err, errChangesNeeded)
	require.Equal(t, 8, strings.Count(out, `"Value": "2"`))
	require.NotContains(t, out, "more lines differ")

	// An error after a differing file must not publish a partial report.
	r.write("z.go", "invalid Go")
	r.commit()
	out, _, err = runVerification("-verify-diff", "HEAD~2..HEAD")
	require.ErrorContains(t, err, "parse")
	require.Empty(t, out)
}
