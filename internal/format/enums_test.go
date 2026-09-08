package format

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEnumUnderlyingTypes keeps typed constants beside their preceding type
// declaration regardless of its underlying type, in both collection modes.
func TestEnumUnderlyingTypes(t *testing.T) {
	tests := []struct {
		name, prelude, declaration, constants string
	}{
		{
			name: "string", declaration: "type T string",
			constants: `const ( A T = "a"; B T = "b" )`,
		},
		{
			name: "boolean", declaration: "type T bool",
			constants: "const ( A T = false; B T = true )",
		},
		{
			name: "integer", declaration: "type T int",
			constants: "const ( A T = iota; B )",
		},
		{
			name: "float", declaration: "type T float64",
			constants: "const ( A T = 1.5; B T = 2.5 )",
		},
		{
			name: "complex", declaration: "type T complex128",
			constants: "const ( A T = 1i; B T = 2i )",
		},
		{
			name: "alias", declaration: "type T = string",
			constants: `const ( A T = "a"; B T = "b" )`,
		},
		{
			name: "derived type", prelude: "type Base string",
			declaration: "type T Base",
			constants:   `const ( A T = "a"; B T = "b" )`,
		},
		{
			name: "conversions", declaration: "type T string",
			constants: `const ( A = T("a"); B = A + "b" )`,
		},
		{
			name: "inherited", declaration: "type T string",
			constants: `const ( A T = "a"; B )`,
		},
		{
			name: "single constant", declaration: "type T bool",
			constants: "const A = T(true)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Put an unrelated function before the type so
			// collecting the constants at the top is observable in
			// the AST order.
			src := []byte(
				"package p\n" + tc.prelude +
					"\n// marker separates collected declarations.\n" +
					"func marker() {}\n" + tc.declaration + "\n" +
					tc.constants + "\n",
			)
			beforeValues, beforeOrder := declarationSemantics(
				t, src,
			)
			out, _, err := Format(src, "test.go", nil)
			require.NoError(t, err)
			requireEnumPlacement(t, out, true)

			// Preserve both values and the fixed point, not just
			// the visual association between the type and its
			// constants.
			afterValues, afterOrder := declarationSemantics(t, out)
			require.Equal(t, beforeValues, afterValues)
			require.Equal(t, beforeOrder, afterOrder)
			again, _, err := Format(out, "test.go", nil)
			require.NoError(t, err)
			require.Equal(t, out, again)

			// Git-scoped formatting uses a separate eligibility
			// pass. Editing only an enum must not collect its
			// declarations.
			scoped, err := CollectChangedDeclarations(
				src, "test.go",
				[]LineRange{{Start: 0, End: 100}},
			)
			require.NoError(t, err)
			require.Equal(t, src, scoped)

			// An unrelated constant can trigger collection of the
			// section, but the enum must still remain with its
			// type.
			withOther := []byte(
				string(src) + "const Unrelated = 42\n",
			)
			scoped, err = CollectChangedDeclarations(
				withOther, "test.go",
				[]LineRange{{Start: 0, End: 100}},
			)
			require.NoError(t, err)
			requireEnumPlacement(t, scoped, true)
		})
	}
}

// TestEnumUnresolvedUnderlyingType preserves the same syntactic association
// when the underlying type comes from another file or package.
func TestEnumUnresolvedUnderlyingType(t *testing.T) {
	for _, underlying := range []string{"Elsewhere", "external.Kind"} {
		src := []byte(
			fmt.Sprintf(`package p

// marker separates collected declarations.
func marker() {}

type T %s

const (
	A T = "a"
	B T = "b"
)
`, underlying),
		)
		out, _, err := Format(src, "test.go", nil)
		require.NoError(t, err)
		requireEnumPlacement(t, out, true)
	}
}

// TestUnrelatedConstantsCollected ensures broadening the underlying type does
// not pin untyped, mixed, or non-adjacent constants beside an unrelated type.
func TestUnrelatedConstantsCollected(t *testing.T) {
	for _, declarations := range []string{
		`type T string; const A = "a"`,
		`type T string; const ( A T = "a"; B = 2 )`,
		`type T string; type U string; const A T = "a"`,
		`type T string; var unrelated = 1; const A T = "a"`,
	} {

		src := []byte("package p\n" + declarations + "\n")
		out, _, err := Format(src, "test.go", nil)
		require.NoError(t, err)
		requireEnumPlacement(t, out, false)

		// Also exercise collection when a different declaration is
		// edited, rather than exempting the entire constant section.
		scoped, err := CollectChangedDeclarations(
			src, "test.go", []LineRange{{Start: 0, End: 100}},
		)
		require.NoError(t, err)
		requireEnumPlacement(t, scoped, false)
	}
}

// requireEnumPlacement checks declaration order without depending on alignment
// or other formatting rules that may change independently of R14.
func requireEnumPlacement(t *testing.T, src []byte, besideType bool) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "test.go", src, 0)
	require.NoError(t, err)

	typeIndex, constIndex := -1, -1
	for i, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		if gd.Tok == token.TYPE {
			for _, spec := range gd.Specs {
				if spec.(*ast.TypeSpec).Name.Name == "T" {
					typeIndex = i
				}
			}
		}
		if gd.Tok == token.CONST {
			constIndex = i
		}
	}
	require.NotEqual(t, -1, typeIndex)
	require.NotEqual(t, -1, constIndex)
	if besideType {
		require.Equal(t, typeIndex+1, constIndex, string(src))
	} else {
		require.Less(t, constIndex, typeIndex, string(src))
	}
}
