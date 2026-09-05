package main

import (
	"bytes"
	"testing"

	"github.com/guggero/goformat/internal/syntax"
)

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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, err := fingerprint([]byte(tt.before))
			if err != nil {
				t.Fatal(err)
			}
			after, err := fingerprint([]byte(tt.after))
			if err != nil {
				t.Fatal(err)
			}

			// The CLI shares these guarantees, while the script
			// stays usable as a standalone standard-library
			// program.
			for _, source := range []string{tt.before, tt.after} {
				standalone, err := fingerprint([]byte(source))
				if err != nil {
					t.Fatal(err)
				}
				shared, err := syntax.Fingerprint([]byte(
					source,
				))
				if err != nil ||
					!bytes.Equal(standalone, shared) {

					t.Fatalf("CLI and standalone syntax "+
						"checks diverged: %v", err)
				}
			}
			if bytes.Equal(before, after) != tt.equal {
				t.Fatalf("equivalence: want %v", tt.equal)
			}
		})
	}
}
