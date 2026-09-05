package format

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/guggero/goformat/internal/config"
)

func TestNoformat(t *testing.T) {
	// Keep the exact input strings together for review.
	//noformat
	cases := []struct {
		name, src, protected string
	}{
		{
			"function",
			"package p\n\n//noformat\nfunc  f( a int,b int ){\n  println( a,b )\n}\n\nfunc g( ){println( 1 )}\n",
			"//noformat\nfunc  f( a int,b int ){\n  println( a,b )\n}\n",
		},
		{
			"statement",
			"package p\nfunc f() {\n //noformat\n  println( 1,2 )\n println( 3,4 )\n}\n",
			" //noformat\n  println( 1,2 )\n",
		},
		{
			"call and closure",
			"package p\nfunc f() {\n //noformat\n run( 1, func( a int,b int ) {\n  println( a,b )\n }, 3,\n )\n println( 3,4 )\n}\n",
			" //noformat\n run( 1, func( a int,b int ) {\n  println( a,b )\n }, 3,\n )\n",
		},
		{
			"argument",
			"package p\nfunc f() {\n run(\n //noformat\n  []int{ 1,2,3 },\n  4,\n )\n println( 3,4 )\n}\n",
			" //noformat\n  []int{ 1,2,3 },\n",
		},
		{
			"raw string",
			"package p\nfunc f() {\n //noformat\n  x := `a {\n //noformat\n b`\n println( x )\n}\n",
			" //noformat\n  x := `a {\n //noformat\n b`\n",
		},
		{
			"blank line",
			"package p\nfunc f() {\n //noformat\n  \n println( 3,4 )\n}\n",
			" //noformat\n  \n",
		},
		{
			"end of file",
			"package p\n//noformat\nfunc  f( ){ }",
			"//noformat\nfunc  f( ){ }",
		},
		{
			"nested directives",
			"package p\n//noformat\nfunc  f( ){\n //noformat\n println( 1,2 )\n}\n",
			"//noformat\nfunc  f( ){\n //noformat\n println( 1,2 )\n}\n",
		},
		{
			"consecutive regions",
			"package p\nfunc f() {\n //noformat\n println( 1,2 )\n // noformat\n println( 3,4 )\n}\n",
			" //noformat\n println( 1,2 )\n // noformat\n println( 3,4 )\n",
		},
	}
	for _, tc := range cases {
		for _, optimize := range []bool{false, true} {
			t.Run(tc.name+fmtBool(optimize), func(t *testing.T) {
				cfg := config.Default()
				cfg.Optimize = optimize
				got, _, err := Format(
					[]byte(tc.src), "test.go", cfg,
				)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(got, []byte(tc.protected)) {
					t.Fatalf("protected bytes "+
						"changed:\n%s", got)
				}
				if _, err := parser.ParseFile(
					token.NewFileSet(), "", got,
					parser.ParseComments,
				); err != nil {

					t.Fatalf("invalid output: %v\n%s", err, got)
				}
				if strings.Contains(string(got), "println( 3,4 )") &&
					tc.name != "consecutive regions" {

					t.Fatalf("unprotected statement was "+
						"not formatted:\n%s", got)
				}
				again, _, err := Format(got, "test.go", cfg)
				if err != nil || !bytes.Equal(got, again) {
					t.Fatalf("not idempotent: %v\n%s", err, again)
				}
			})
		}
	}
}

func fmtBool(optimize bool) string {
	if optimize {
		return "/optimize"
	}
	return "/default"
}

func TestNoformatDiagnostics(t *testing.T) {
	src := "package p\nfunc f() " +
		"{\n//noformat\nprintln(\"" +
		strings.Repeat("x", 100) + "\")\n// " +
		strings.Repeat("y", 100) + "\n}\n"
	_, diags, err := Format([]byte(src), "test.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || diags[0].Rule != "R10" {
		t.Fatalf("want only the unprotected long comment diagnostic, "+
			"got %v", diags)
	}
}

func TestNoformatAdditionalContexts(t *testing.T) {
	// Keep the exact input strings together for review.
	//noformat
	cases := []struct{ before, protected, after string }{
		{"", "//noformat\npackage   p\n", "\nfunc f( ){ }\n"},
		{"package p\n", "//noformat\nimport (\n  \"strings\"\n \"bytes\"\n)\n", "\nfunc f( ){ }\n"},
		{"package p\nimport (\n", " //noformat\n  \"strings\"\n", " \"bytes\"\n)\nfunc f( ){ }\n"},
		{"package p\ntype T struct {\n", " //noformat\n   A,B   int\n", " C  int\n}\n"},
		{"package p\nfunc f() {\n", " //noformat\n if true {\n  println( 1 )\n } else {\n  println( 2 )\n }\n", "println( 3 )\n}\n"},
		{"package p\nfunc f() {\n", " //noformat\n x := 1 +\n", "  2\nprintln( x )\n}\n"},
		{"package p\nfunc f() {\n", " //noformat\n x := `a\r\nb\r\nc\r\nd\r\ne`\n", "println( x )\n}\n"},
		{"package p\n", "//noformat", ""},
		{"package p\n//noformat:goformat:0:begin\n", "//noformat\nvar  x=1\n", "var y = 2\n"},
	}
	for i, tc := range cases {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			cfg := config.Default()
			cfg.Optimize = true
			src := []byte(tc.before + tc.protected + tc.after)
			got, _, err := Format(src, "test.go", cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(got, []byte(tc.protected)) {
				t.Fatalf("protected bytes changed:\n%s", got)
			}
			if _, err := parser.ParseFile(
				token.NewFileSet(), "", got,
				parser.ParseComments,
			); err != nil {

				t.Fatalf("invalid output: %v\n%s", err, got)
			}
			if len(noformatRanges(got)) != 1 {
				t.Fatalf("directive lost:\n%s", got)
			}
			again, _, err := Format(got, "test.go", cfg)
			if err != nil || !bytes.Equal(got, again) {
				t.Fatalf("not idempotent: %v\n%s\n%s", err, got, again)
			}
		})
	}
}

func TestNoformatAfterGodoc(t *testing.T) {
	for _, directive := range []string{"//noformat", "// noformat"} {
		for _, declaration := range []string{
			"const (\n  x=1\n)\n",
			"var (\n  x=1\n)\n",
			"func  f( ){ println( 1 ) }\n",
		} {

			prefix := `package p

import "fmt"

type T struct{}

// Documentation for the block.
`
			suffix := "\n// Following function.\nfunc g() {}\n"
			src := []byte(
				prefix + directive + "\n" + declaration + suffix,
			)
			for _, optimize := range []bool{false, true} {
				cfg := config.Default()
				cfg.Optimize = optimize
				got, _, err := Format(src, "test.go", cfg)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(src, got) {
					t.Fatalf("documented protected "+
						"declaration changed:\n%s", got)
				}
			}
		}
	}
}

// The real VHTLC block exercises the printer's Godoc normalization, which
// can move a prose directive across an adjacent internal directive marker.
func TestNoformatVHTLCBlock(t *testing.T) {
	src, err := os.ReadFile(
		"../../testdata/noformat/documented_block.in.go",
	)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(
		"../../testdata/noformat/documented_block.out.go",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, optimize := range []bool{false, true} {
		cfg := config.Default()
		cfg.Optimize = optimize
		got := src
		for i := 0; i < 3; i++ {
			got, _, err = Format(got, "vhtlc.go", cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("protected block changed on run "+
					"%d:\n%s", i, got)
			}
		}
	}
}
