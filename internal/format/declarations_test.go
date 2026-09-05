package format

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/guggero/goformat/internal/config"
)

// Check actual constant values and type-checker initialization order, rather
// than just accepting syntactically valid output from declaration movement.
func TestDeclarationSemantics(t *testing.T) {
	sources := []string{
		`package p
func f() {}
var firstLongCounter, secondLongCounter, thirdLongCounter, fourthLongCounter, fifthLongCounter [4]int
var keep = 1
`,
		`package p
type Mode uint8
const ( Off Mode = iota; On )
type Level int
const Lowest = Level(1)
type Alias = Level
const ( Low Alias = 1; High = Low + 1 )
`,
		`package p
var order = 0
func next() int { order++; return order }
var first = next()
func f() {}
const ( A = iota; B; C = iota * 10 )
var second = next()
const ( D = iota + 20; E )
const F = iota
const ( G = 42; H )
var third = next()
`,
		`package p
var order = 0
func next() int { order++; return order }
var first = next()
//noformat
var   middle=next()
func f() {}
var last = next()
`,
		`package p
var order = 0
func next() int { order++; return order }
var first = next()
var _ int = next()
func f() {}
var last = next()
`,
		`package p
var order = 0
func next() int { order++; return order }
var first = next()
var (
 second = next()
 _ int = next()
 third = next()
)
func f() {}
var last = next()
`,
		`package p
var first = later
func f() {}
var count int
var later = next()
func next() int { count++; return count }
var last = next()
`,
		`package p; const A = 1; var B = 2; func f() {}; const C = 3`,
		"package p\nconst A = `first\n  second\nthird`\nvar B = A\n",
	}
	for i, src := range sources {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			beforeValues, beforeOrder := declarationSemantics(
				t, []byte(src),
			)
			out, _, err := Format([]byte(src), "test.go", nil)
			if err != nil {
				t.Fatal(err)
			}
			afterValues, afterOrder := declarationSemantics(t, out)
			if !reflect.DeepEqual(beforeValues, afterValues) ||
				!reflect.DeepEqual(beforeOrder, afterOrder) {

				t.Fatalf("semantics changed: values %v -> %v; "+
					"order %v -> %v\n%s", beforeValues, afterValues, beforeOrder, afterOrder, out)
			}
			again, _, err := Format(out, "test.go", nil)
			if err != nil || !bytes.Equal(out, again) {
				t.Fatalf("not idempotent: %v\n%s\n%s", err, out, again)
			}
		})
	}
}

func declarationSemantics(t *testing.T, src []byte) ([]string, []string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{}
	cfg := types.Config{}
	pkg, err := cfg.Check("p", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("type check: %v\n%s", err, src)
	}
	var values, order []string
	for _, name := range pkg.Scope().Names() {
		obj := pkg.Scope().Lookup(name)
		value := name + ":" + obj.Type().String()
		if c, ok := obj.(*types.Const); ok {
			value += "=" + c.Val().ExactString()
		}
		values = append(values, value)
	}
	sort.Strings(values)
	for _, init := range info.InitOrder {
		var names []string
		for _, variable := range init.Lhs {
			names = append(names, variable.Name())
		}
		order = append(
			order,
			strings.Join(names, ",")+"="+types.ExprString(init.Rhs),
		)
	}
	return values, order
}

func TestDeclarationGroupingDisabled(t *testing.T) {
	src := []byte("package p\n\nfunc f() {}\n\nvar x = 1\n")
	cfg := config.Default()
	disabled := false
	cfg.Rules.DeclarationGrouping = &disabled
	out, _, err := Format(src, "test.go", cfg)
	if err != nil || !bytes.Equal(src, out) {
		t.Fatalf("disabled grouping changed source: %v\n%s", err, out)
	}
}
