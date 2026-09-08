package format

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/syntax"
	"github.com/stretchr/testify/require"
)

// TestR7MultilineFields requires every field in a multiline keyed literal to
// occupy its own line, even when all existing lines fit the width limit.
func TestR7MultilineFields(t *testing.T) {
	cases := map[string]string{
		"wallet config": `cfg := wavewalletdk.Config{
	DataDir: t.TempDir(), Network: "regtest", DebugLevel: "debug",
	ServerAddress: harness.WavelengthAddress, ServerInsecure: true,
	SwapServerAddress: harness.WavelengthAddress, SwapServerInsecure: true,
	WalletEsploraURL:     "http://127.0.0.1:3000",
	WalletPollInterval:   harness.NormalPollInterval,
	WalletRecoveryWindow: 10,
}`,
		"short packed fields": `cfg := T{
	A: 1, B: 2,
}`,
		"opening and closing line": `cfg := T{A: 1,
	B: 2, C: 3}`,
		"nested initializer": `cfg := T{
	A: U{
		X: 1, Y: 2,
	}, B: 3,
}`,
		"elided type": `cfg := []T{{
	A: 1, B: 2,
}}`,
		"multiline field value": `cfg := T{A: func() {
	work()
}, B: 2}`,
		"field comments": `cfg := T{
	// Keep the first field's purpose attached to it.
	A: 1, B: 2, // Keep this trailing explanation.

	// Keep this group separate from the first group.
	C: 3, D: 4,
}`,
	}
	for name, literal := range cases {
		t.Run(name, func(t *testing.T) {
			src := []byte(
				"package p\n\n// f sets up a fixture.\n" +
					"func f() {\n" + literal + "\n}\n",
			)
			for _, onlyR7 := range []bool{false, true} {
				cfg := config.Default()
				if onlyR7 {
					require.NoError(t, cfg.SelectRules(
						[]string{
							"R7",
						},
					))
				}

				// Check the public formatting result rather
				// than dst decorations, including interactions
				// with other passes.
				out, _, err := Format(src, "test.go", cfg)
				require.NoError(t, err)
				requireSeparateFields(t, out)
				before, err := syntax.Fingerprint(src)
				require.NoError(t, err)
				after, err := syntax.Fingerprint(out)
				require.NoError(t, err)
				require.Equal(t, before, after)

				// Enforcing field boundaries must reach a fixed
				// point even when a value has a multiline call
				// or literal.
				again, _, err := Format(out, "test.go", cfg)
				require.NoError(t, err)
				require.Equal(t, out, again)
			}
		})
	}
}

// TestR7PreservesValidLayouts prevents field expansion from churning fitting
// single-line literals, slice packing, or meaningful blank lines and comments.
func TestR7PreservesValidLayouts(t *testing.T) {
	for _, body := range []string{
		"\tcfg := T{A: 1, B: 2}\n",
		"\tcfg := T{}\n",
		"\tcfg := []int{\n\t\t1, 2, 3,\n\t\t4, 5, 6,\n\t}\n",
		`	cfg := T{
		A: 1, // Keep this explanation with A.

		// This group has a separate purpose.
		B: 2,
	}
`,
	} {

		src := []byte(
			"package p\n\n// f sets up a fixture.\n" +
				"func f() {\n" + body + "}\n",
		)
		out, _, err := Format(src, "test.go", nil)
		require.NoError(t, err)
		require.Equal(t, string(src), string(out))
	}
}

// TestR7DisabledAndProtected honors explicit opt-outs even when the literal
// would otherwise need its packed fields expanded.
func TestR7DisabledAndProtected(t *testing.T) {
	for _, protected := range []bool{false, true} {
		body := "\tcfg := T{\n\t\tA: 1, B: 2,\n\t}\n"
		cfg := config.Default()
		if protected {
			body = "\t//noformat\n" + body
		} else {
			disabled := false
			cfg.Rules.InlineCompositeLit = &disabled
		}
		src := []byte(
			"package p\n\n// f sets up a fixture.\n" +
				"func f() {\n" + body + "}\n",
		)
		out, _, err := Format(src, "test.go", cfg)
		require.NoError(t, err)
		require.Equal(t, string(src), string(out))
	}
}

// requireSeparateFields checks physical field boundaries in rendered Go while
// allowing a complete nested initializer to fit on one line.
func requireSeparateFields(t *testing.T, src []byte) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	require.NoError(t, err)
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok || len(literal.Elts) == 0 {
			return true
		}
		if _, keyed := literal.Elts[0].(*ast.KeyValueExpr); !keyed {
			return true
		}
		if fset.Position(literal.Pos()).Line ==
			fset.Position(literal.Rbrace).Line {

			return true
		}

		previous := fset.Position(literal.Lbrace).Line
		for _, field := range literal.Elts {
			require.Greater(
				t, fset.Position(field.Pos()).Line, previous,
				string(src),
			)
			previous = fset.Position(field.End()).Line
		}
		require.Greater(
			t, fset.Position(literal.Rbrace).Line, previous,
			string(src),
		)
		return true
	})
}
