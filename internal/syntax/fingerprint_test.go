package syntax

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVariableGrouping(t *testing.T) {
	before, err := Fingerprint([]byte(
		"package p; func f() { var a, b [4]int }",
	))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source string
		equal  bool
	}{
		{"package p; func f() { var (a [4]int; b [4]int) }", true},
		{"package p; func f() { var (a [4]int; b []int) }", false},
		{"package p; func f() { var (b [4]int; a [4]int) }", false},
	} {

		after, err := Fingerprint([]byte(tc.source))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(before, after) != tc.equal {
			t.Fatalf("incorrect equivalence for %s", tc.source)
		}
	}
}

// TestFingerprint accepts formatting changes while preserving syntax, literal
// bytes, and meaningful comments used by the commit verifier and scoped
// formatter.
func TestFingerprint(t *testing.T) {
	tests := []struct {
		name, before, after string
		equal               bool
	}{
		{
			name:   "wrap and trailing comma",
			before: "package p\nfunc f() { call(a, b) }",
			after:  "package p\nfunc f() {\n call(\n a, b,\n )\n}",
			equal:  true,
		},
		{
			name:   "literal concat",
			before: `package p; var s = "first " + "second"`,
			after:  `package p; var s = "first second"`,
			equal:  true,
		},
		{
			name:   "comment wrapping",
			before: "package p\n// Some long comment.\nvar n = 1",
			after: "package p\n// Some long\n// comment.\nvar n " +
				"= 1",
			equal: true,
		},
		{
			name:   "argument change",
			before: "package p; func f() { call(a, b) }",
			after:  "package p; func f() { call(b, a) }",
		},
		{
			name:   "whitespace inside string",
			before: `package p; var s = "first " + "second"`,
			after:  `package p; var s = "firstsecond"`,
		},
		{
			name:   "raw string whitespace",
			before: "package p; var s = `a b`",
			after:  "package p; var s = `a  b`",
		},
		{
			name:   "non-UTF8 strings",
			before: `package p; var s = "\xff"`,
			after:  `package p; var s = "\xfe"`,
		},
		{
			name:   "variadic call",
			before: "package p; func f() { call(xs...) }",
			after:  "package p; func f() { call(xs) }",
		},
		{
			name:   "type alias",
			before: "package p; type T = U",
			after:  "package p; type T U",
		},
		{
			name:   "semicolon insertion",
			before: "package p; func f() int { return g() }",
			after:  "package p; func f() int { return\ng() }",
		},
		{
			name:   "build directive",
			before: "//go:build linux\n\npackage p",
			after:  "//go:build darwin\n\npackage p",
		},
		{
			name: "directive attachment",
			before: "package p\n//go:embed file\nvar a " +
				"string\nvar b string",
			after: "package p\nvar a string\n//go:embed " +
				"file\nvar b string",
		},
		{
			name: "cgo preamble",
			before: "package p\n/* char *s = \"a b\"; */\nimport " +
				"\"C\"",
			after: "package p\n/* char *s = \"a  b\"; */\nimport " +
				"\"C\"",
		},
	}

	// Exercise the shared checker directly so its guarantees remain covered
	// without maintaining a second implementation just for comparison.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, err := Fingerprint([]byte(tt.before))
			require.NoError(t, err)

			after, err := Fingerprint([]byte(tt.after))
			require.NoError(t, err)
			require.Equal(t, tt.equal, bytes.Equal(before, after))
		})
	}
}
