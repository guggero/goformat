package format

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/config"
)

var (
	// printfVerb recognizes value-consuming fmt directives, including
	// indexed operands and dynamic widths. Escaped percent signs are
	// handled separately.
	printfVerb = regexp.MustCompile(
		`^%[-+# 0]*(?:[0-9]+|\[[1-9][0-9]*\]\*|\*)?` +
			`(?:\.(?:[0-9]+|\[[1-9][0-9]*\]\*|\*)?)?` +
			`(?:\[[1-9][0-9]*\])?[vTtbcdoOqxXUeEfFgGsp]`,
	)
)

// OptimizeRequireCalls upgrades eligible Testify require calls to their f
// variants without changing any other bytes. Only optimization with R5 enabled
// permits these edits. The formatting allow/deny lists select eligible pairs;
// import resolution, message positions, and supplied values constrain them.
// Protected regions and nolint functions remain untouched. The CLI also uses
// this normalization to validate edits limited to selected source lines.
func OptimizeRequireCalls(src []byte, cfg *config.Config) ([]byte, error) {
	if !cfg.Optimize || !cfg.Rules.FormattingFnCompactOn() {
		return src, nil
	}

	// Parse before layout changes so identifier bindings and insertion
	// offsets refer to the original bytes. Checking bindings prevents a
	// parameter or local variable named require from being mistaken for
	// the imported package, including when that import has an alias.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	protected := noformatRanges(src)
	var insertions []int
	ast.Inspect(file, func(node ast.Node) bool {
		if fn, ok := node.(*ast.FuncDecl); ok && hasNolint(fn.Doc) {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok || call.Ellipsis.IsValid() {
			return true
		}
		index, ok := requireMessageIndex(file, call)
		if !ok || len(call.Args) <= index+1 {
			return true
		}
		sel := call.Fun.(*ast.SelectorExpr)
		if strings.HasSuffix(sel.Sel.Name, "f") {
			return true
		}

		// The list grants the compact exception to the target name.
		// Honor canonical package names as well as explicit aliases,
		// while leaving calls denied by either spelling alone.
		target := calleeName(call) + "f"
		canonical := "require." + sel.Sel.Name + "f"
		if !inStringSet(target, cfg.FormattingFuncs) &&
			!inStringSet(canonical, cfg.FormattingFuncs) {

			return true
		}
		if inStringSetExact(target, cfg.FormattingFuncsDeny) ||
			inStringSetExact(canonical, cfg.FormattingFuncsDeny) {

			return true
		}
		message, ok := literalMessage(call.Args[index])
		if !ok || !hasPrintfVerb(message) {
			return true
		}

		// A protected descendant can constrain the whole call's layout.
		// Match the normal formatter's conservative protection behavior
		// rather than merely testing the selector's insertion point.
		start := fset.Position(call.Pos()).Offset
		end := fset.Position(call.End()).Offset
		for _, region := range protected {
			if start < region.end && end > region.start {
				return true
			}
		}
		insertions = append(
			insertions, fset.Position(sel.Sel.End()).Offset,
		)
		return true
	})

	// Apply edits in source order so nested calls and comments retain all
	// their original text. The subsequent parse sees the longer names and
	// therefore gives layout passes accurate source columns.
	sort.Ints(insertions)
	var out bytes.Buffer
	start := 0
	for _, offset := range insertions {
		out.Write(src[start:offset])
		out.WriteByte('f')
		start = offset
	}
	if len(insertions) == 0 {
		return src, nil
	}
	out.Write(src[start:])
	return out.Bytes(), nil
}

// requireMessageIndex identifies package-level Testify assertions and returns
// their first optional message argument. Unknown functions, dot imports, and
// bound Assertions methods are intentionally excluded without type information.
func requireMessageIndex(file *ast.File, call *ast.CallExpr) (int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return 0, false
	}
	qualifier, ok := sel.X.(*ast.Ident)
	if !ok || qualifier.Obj != nil {
		return 0, false
	}
	imported := false
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != "github.com/stretchr/testify/require" {
			continue
		}
		name := "require"
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imported = name == qualifier.Name
		break
	}
	if !imported {
		return 0, false
	}

	// Count required parameters, including TestingT, rather than searching
	// for the first string: expected values and error text may be strings
	// containing percent signs but are not assertion messages.
	switch strings.TrimSuffix(sel.Sel.Name, "f") {
	case "Condition", "DirExists", "Empty", "Error", "Fail", "FailNow",
		"False", "FileExists", "IsDecreasing", "IsIncreasing",
		"IsNonDecreasing", "IsNonIncreasing", "Negative", "Nil",
		"NoDirExists", "NoError", "NoFileExists", "NotEmpty", "NotNil",
		"NotPanics", "NotZero", "Panics", "Positive", "True", "Zero":

		return 2, true

	case "Contains", "ElementsMatch", "Equal", "EqualError",
		"EqualExportedValues", "EqualValues", "ErrorAs",
		"ErrorContains", "ErrorIs", "Exactly", "Greater", "GreaterOrE" +
			"qual",
		"Implements",
		"IsType", "JSONEq", "Len", "Less", "LessOrEqual", "NotContains",
		"NotElementsMatch", "NotEqual", "NotEqualValues", "NotErrorAs",
		"NotErrorIs", "NotImplements", "NotRegexp", "NotSame",
		"NotSubset", "PanicsWithError", "PanicsWithValue", "Regexp",
		"Same", "Subset", "YAMLEq":

		return 3, true

	case "Eventually", "EventuallyWithT", "InDelta", "InDeltaMapValues",
		"InDeltaSlice", "InEpsilon", "InEpsilonSlice", "Never",
		"WithinDuration", "WithinRange":

		return 4, true

	case "HTTPError", "HTTPRedirect", "HTTPSuccess":
		return 5, true

	case "HTTPBodyContains", "HTTPBodyNotContains", "HTTPStatusCode":
		return 6, true
	}
	return 0, false
}

// literalMessage evaluates only string literals and their concatenations.
// Constants and arbitrary expressions are left alone without type checking.
func literalMessage(expr ast.Expr) (string, bool) {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		if expr.Kind == token.STRING {
			value, err := strconv.Unquote(expr.Value)
			return value, err == nil
		}

	case *ast.BinaryExpr:
		if expr.Op == token.ADD {
			left, leftOK := literalMessage(expr.X)
			right, rightOK := literalMessage(expr.Y)
			return left + right, leftOK && rightOK
		}

	case *ast.ParenExpr:
		return literalMessage(expr.X)
	}
	return "", false
}

// hasPrintfVerb distinguishes consuming directives from escaped percent signs.
// A literal %% does not justify switching to a formatting assertion.
func hasPrintfVerb(message string) bool {
	for i := 0; i < len(message); i++ {
		if message[i] != '%' {
			continue
		}
		if i+1 < len(message) && message[i+1] == '%' {
			i++
			continue
		}
		if printfVerb.MatchString(message[i:]) {
			return true
		}
	}
	return false
}

// applyRequireFormattingLayout budgets compact assertion messages after their
// required operands and before the formatting values. Complex arguments and
// commented or escaped message expressions retain their existing layout.
func applyRequireFormattingLayout(ctx *Context, ac *ast.CallExpr,
	call *dst.CallExpr, index, limit, tab int) bool {

	if len(call.Args) <= index || call.Ellipsis {
		return false
	}
	if !ctx.Config.Optimize && allCallLinesFit(ctx, ac, limit, tab) {
		return false
	}

	// Compact only when all surrounding operands fit on their respective
	// lines. Their widths, separators, closing parenthesis, and any text
	// following the call must be reserved before splitting the message.
	fset, lines := ctx.FileSet, ctx.SourceLines
	widths := argWidths(fset, lines, ac.Args, tab)
	first := visualCol(fset, lines, ac.Pos(), tab) + sourceWidth(
		fset, lines, ac.Fun.Pos(), ac.Fun.End(), tab,
	) + 1
	tail := 1 + postCallLineWidth(fset, lines, ac.End(), tab)
	for i, width := range widths {
		if i == index {
			continue
		}
		if width >= wideForcedBreak {
			return false
		}
		if i < index {
			first += width + 2
		} else {
			tail += width + 2
		}
	}
	indent := lineIndentAt(fset, lines, ac.Pos(), tab) + tab
	firstNonLast, firstLast := limit-first-3, limit-first-2-tail
	contNonLast, contLast := limit-indent-3, limit-indent-2-tail
	if firstNonLast < 2 || contNonLast < 2 || contLast < 1 {
		return false
	}

	// Rebuilding a literal-only chain is safe only when no internal
	// comments or escape sequences can be lost or split in the process.
	message := call.Args[index]
	var chunks []string
	if !collectStringChunks(message, &chunks) {
		return false
	}
	commented := false
	dst.Inspect(message, func(node dst.Node) bool {
		if node != nil {
			decs := node.Decorations()
			commented = commented || len(decs.Start) > 0 ||
				len(decs.End) > 0
			if expr, ok := node.(*dst.BinaryExpr); ok {
				commented = commented || len(expr.Decs.X) > 0 ||
					len(expr.Decs.Op) > 0
			}
		}
		return true
	})
	body := strings.Join(chunks, "")
	if commented || strings.ContainsRune(body, '\\') {
		return false
	}

	// Gofmt prints concatenation without spaces around '+' inside call
	// arguments. Count that exact frame and reserve all trailing values
	// on the last chunk's line, rather than assuming the message is arg 0.
	chunks = multiSplit(
		body, firstNonLast, firstLast, contNonLast, contLast,
	)
	if len(chunks) == 0 {
		return false
	}
	for i, chunk := range chunks {
		budget := contNonLast
		if i == 0 {
			budget = firstNonLast
		}
		if i == len(chunks)-1 {
			budget = contLast
			if i == 0 {
				budget = firstLast
			}
		}
		if len(chunk) > budget {
			return false
		}
	}

	var replacement dst.Expr
	if len(chunks) == 1 {
		replacement = &dst.BasicLit{
			Kind:  token.STRING,
			Value: `"` + body + `"`,
		}
	} else {
		replacement = buildPlusChain(chunks)
	}
	copyOuterDecs(message, replacement)
	call.Args[index] = replacement
	clearCallArgLayout(call)
	return true
}
