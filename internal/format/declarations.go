package format

import (
	"bytes"
	"fmt"
	"go/ast"
	gofmt "go/format"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// LineRange identifies zero-based source lines with an exclusive End.
type LineRange struct {
	Start, End int
}

// CollectChangedDeclarations applies the collection rule only when an eligible
// package declaration was edited. Organizing its section may move declarations
// outside the changed lines, while other source bytes are left untouched.
func CollectChangedDeclarations(src []byte, filename string,
	changed []LineRange) ([]byte, error) {

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	types := declarationTypes(file)
	protected := noformatRanges(src)
	constants, variables := false, false
	var previous ast.Decl
	for _, declaration := range file.Decls {
		prev := previous
		previous = declaration
		gd, ok := declaration.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		start := gd.Pos()
		if gd.Doc != nil {
			start = gd.Doc.Pos()
		}
		first := fset.PositionFor(start, false)
		last := fset.PositionFor(gd.End(), false)
		touched := false
		for _, r := range changed {
			touched = touched ||
				(first.Line-1 < r.End && last.Line > r.Start)
		}
		for _, r := range protected {
			if first.Offset < r.end && last.Offset > r.start {
				touched = false
				break
			}
		}
		if !touched || (gd.Tok == token.CONST && isEnumDeclaration(
			gd, prev, types,
		)) {

			continue
		}
		if gd.Tok == token.CONST {
			constants = true
		} else {
			for _, spec := range gd.Specs {
				variables = variables ||
					!isTypeAssertion(spec.(*ast.ValueSpec))
			}
		}
	}
	if !constants && !variables {
		return src, nil
	}
	return collectDeclarationsWithOptions(
		src, filename, constants, variables, true,
	)
}

func declarationTypes(file *ast.File) map[string]ast.Expr {
	types := make(map[string]ast.Expr)
	for _, declaration := range file.Decls {
		if gd, ok := declaration.(*ast.GenDecl); ok &&
			gd.Tok == token.TYPE {

			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				types[ts.Name.Name] = ts.Type
			}
		}
	}
	return types
}

// collectDeclarations runs before decoration so relocated documentation gets
// fresh source positions. It retains specification order, and treats protected
// declarations and effectful assertions as initialization-order barriers.
func collectDeclarations(src []byte, filename string) ([]byte, error) {
	return collectDeclarationsWithOptions(src, filename, true, true, false)
}

func collectDeclarationsWithOptions(src []byte, filename string,
	constantsOn, variablesOn, tidy bool) ([]byte, error) {

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	file := fset.File(f.Pos())
	offset := file.Offset
	protected := noformatRanges(src)
	types := declarationTypes(f)

	// Include a trailing comment in a moved declaration's source span.
	declEnd := func(d ast.Node) int {
		end := offset(d.End())
		for i := end; i < len(src); i++ {
			if src[i] == ';' {
				end = i + 1
				break
			}
			if src[i] != ' ' && src[i] != '\t' {
				break
			}
		}
		for _, cg := range f.Comments {
			commentStart := offset(cg.Pos())
			sameLine := file.Line(cg.Pos()) == file.Line(d.End())
			if commentStart < end || !sameLine {
				continue
			}
			if len(bytes.TrimSpace(src[end:commentStart])) == 0 {
				end = offset(cg.End())
			}
		}
		return end
	}
	anchor := declEnd(f.Name)
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			anchor = declEnd(gd)
		}
	}

	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	var constGroups []string
	var constants []string
	var variables []string
	varAnchor := anchor
	if variablesOn && !constantsOn {
		// Unchanged leading const blocks stay ahead of the variable
		// section.
		for _, declaration := range f.Decls {
			gd, ok := declaration.(*ast.GenDecl)
			if !ok || (gd.Tok != token.IMPORT && gd.Tok != token.CONST && gd.Tok != token.VAR) {
				break
			}
			if gd.Tok == token.CONST {
				varAnchor = declEnd(gd)
			}
		}
	}
	flushConstants := func() {
		if len(constants) > 0 {
			constGroups = append(constGroups, declarationBlock(
				"const", constants,
			))
			constants = nil
		}
	}
	varBlocks := make(map[int][]string)
	flushVariables := func() {
		if len(variables) > 0 {
			varBlocks[varAnchor] = append(
				varBlocks[varAnchor],
				declarationBlock("var", variables),
			)
			variables = nil
		}
	}
	var previous ast.Decl
	for _, d := range f.Decls {
		prev := previous
		previous = d
		gd, ok := d.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) ||
			len(gd.Specs) == 0 {

			continue
		}
		if (gd.Tok == token.CONST && !constantsOn) ||
			(gd.Tok == token.VAR && !variablesOn) {

			continue
		}
		start, end := offset(gd.Pos()), declEnd(gd)
		if gd.Doc != nil {
			start = offset(gd.Doc.Pos())
		}
		pinned := false
		for _, r := range protected {
			if start < r.end && end > r.start {
				pinned = true
			}
		}
		if pinned {
			if gd.Tok == token.VAR {
				flushVariables()

				// Include the complete protected range,
				// including its trailing newline, in the
				// insertion boundary.
				varAnchor = end
				for _, r := range protected {
					if start < r.end && end > r.start &&
						r.end > varAnchor {

						varAnchor = r.end
					}
				}
			}
			continue
		}
		if gd.Tok == token.CONST && isEnumDeclaration(gd, prev, types) {
			continue
		}

		// A header documents its existing block, not its first
		// specification. Keep that block intact when collecting it,
		// rather than merging its header into another block's element
		// documentation.
		documentedBlock := gd.Lparen.IsValid() && gd.Doc != nil
		hasAssertion := false
		if gd.Tok == token.VAR {
			for _, spec := range gd.Specs {
				hasAssertion = hasAssertion ||
					isTypeAssertion(spec.(*ast.ValueSpec))
			}
		}
		if documentedBlock && !hasAssertion {
			block := string(src[start:end])
			if gd.Tok == token.CONST {
				flushConstants()
				constGroups = append(constGroups, block)
			} else {
				flushVariables()
				varBlocks[varAnchor] = append(
					varBlocks[varAnchor], block,
				)
			}
			edits = append(edits, edit{start, end, ""})
			continue
		}

		chunks := declarationChunks(src, gd, offset)
		if gd.Lparen.IsValid() && end > offset(gd.End()) {
			tail := strings.TrimSpace(string(
				src[offset(gd.End()):end],
			))
			tail = strings.TrimSpace(strings.TrimPrefix(tail, ";"))
			if tail != "" {
				chunks[len(chunks)-1] += "\n" + tail
			}
		}
		if gd.Tok == token.CONST {
			if usesIota(gd) {
				flushConstants()
				constGroups = append(
					constGroups,
					declarationBlock("const", chunks),
				)
			} else {
				constants = append(constants, chunks...)
			}
			edits = append(edits, edit{start, end, ""})
			continue
		}

		// Split mixed blocks without detaching comments from
		// assertions. An assertion with a nontrivial initializer pins
		// the entire block: partitioning it could otherwise reorder
		// side effects.
		barrier := false
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			if isTypeAssertion(vs) && !inertAssertion(vs) {
				barrier = true
			}
		}
		if barrier {
			flushVariables()
			varAnchor = end
			continue
		}
		var assertions []string
		for i, spec := range gd.Specs {
			if isTypeAssertion(spec.(*ast.ValueSpec)) {
				assertions = append(assertions, chunks[i])
			} else {
				variables = append(variables, chunks[i])
			}
		}
		if len(assertions) == len(gd.Specs) {
			continue
		}
		replacement := ""
		if len(assertions) > 0 {
			replacement = declarationBlock("var", assertions)
			if documentedBlock {
				header := src[offset(
					gd.Doc.Pos(),
				):offset(
					gd.Doc.End(),
				)]
				replacement = string(header) + "\n" +
					replacement
			}
		}
		edits = append(edits, edit{start, end, replacement})
	}
	flushConstants()
	flushVariables()
	if len(constGroups) > 0 {
		varBlocks[anchor] = append(constGroups, varBlocks[anchor]...)
	}
	for pos, blocks := range varBlocks {
		edits = append(edits, edit{
			pos, pos,
			"\n\n" + strings.Join(blocks, "\n\n") + "\n\n",
		})
	}
	trimEOF := false
	if tidy {
		for i := range edits {
			e := &edits[i]
			if e.start == e.end {
				// Replace separator whitespace at the insertion
				// point, rather than accumulating blank lines
				// on each scoped formatting run.
				for j := e.end; j < len(src); j++ {
					if src[j] != ' ' && src[j] != '\t' &&
						src[j] != '\r' &&
						src[j] != '\n' {

						break
					}
					if src[j] == '\n' {
						e.end = j + 1
					}
				}
				continue
			}
			start, end := lineStart(
				src, e.start,
			), lineEnd(
				src, e.end,
			)
			if len(bytes.TrimSpace(src[start:e.start])) != 0 ||
				len(bytes.TrimSpace(src[e.end:end])) != 0 {

				continue
			}
			e.start, e.end = start, end
			trimEOF = trimEOF || (end == len(src) && e.text == "")
			if end < len(src) {
				next := lineEnd(src, end)
				if len(bytes.TrimSpace(src[end:next])) == 0 {
					e.end = next
				}
			}
			if e.text != "" {
				e.text = strings.TrimRight(e.text, "\n") +
					"\n\n"
			}
		}
	}
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start == edits[j].start {
			return edits[i].end < edits[j].end
		}
		return edits[i].start < edits[j].start
	})
	var out bytes.Buffer
	prev := 0
	for _, e := range edits {
		if e.start < prev {
			return nil, fmt.Errorf("%s: overlapping declaration "+
				"edits", filename)
		}
		out.Write(src[prev:e.start])
		out.WriteString(e.text)
		prev = e.end
	}
	out.Write(src[prev:])
	result := out.Bytes()
	if trimEOF {
		// Removing a final declaration also removes its now-unused
		// separator. Preserve all bytes on the preceding code line,
		// including noformat.
		for len(result) > 0 {
			start := lineStart(result, len(result)-1)
			if len(bytes.TrimSpace(result[start:])) != 0 {
				break
			}
			result = result[:start]
		}
	}
	return result, nil
}

func declarationBlock(kind string, chunks []string) string {
	var body strings.Builder
	for i, chunk := range chunks {
		if i > 0 {
			body.WriteByte('\n')
			if strings.HasSuffix(chunks[i-1], "\n") ||
				strings.HasPrefix(chunk, "//") ||
				strings.HasPrefix(chunk, "/*") {

				body.WriteByte('\n')
			}
		}
		body.WriteString(strings.TrimRight(chunk, "\n"))
	}
	block := kind + " (\n" + body.String() + "\n)"

	// Establish the new indentation before the width-sensitive passes
	// inspect source positions. No protected specifications enter a
	// collected block.
	formatted, err := gofmt.Source([]byte("package p\n" + block))
	if err != nil {
		// The complete-file parser will report a contextual error to
		// the caller.
		return block
	}
	return strings.TrimSpace(string(
		formatted[bytes.IndexByte(formatted, '\n')+1:],
	))
}

// declarationChunks keeps the raw contents of each specification, including
// free-standing and trailing comments. Whitespace inside raw literals is never
// adjusted here; the normal printer handles indentation of the code itself.
func declarationChunks(src []byte, gd *ast.GenDecl,
	offset func(token.Pos) int) []string {

	start := offset(gd.Pos()) + len(gd.Tok.String())
	end := offset(gd.End())
	if gd.Lparen.IsValid() {
		start = offset(gd.Lparen) + 1
		end = offset(gd.Rparen)
	}
	var chunks []string
	for i, spec := range gd.Specs {
		stop := end
		if i+1 < len(gd.Specs) {
			next := gd.Specs[i+1].(*ast.ValueSpec)
			stop = offset(next.Pos())
			if next.Doc != nil {
				stop = offset(next.Doc.Pos())
			}
		}
		if i == len(gd.Specs)-1 {
			vs := spec.(*ast.ValueSpec)
			if vs.Comment != nil &&
				offset(vs.Comment.End()) > stop {

				stop = offset(vs.Comment.End())
			}
		}
		raw := string(src[start:stop])
		chunk := strings.TrimSpace(raw)
		trailing := raw[len(strings.TrimRight(raw, " \t\r\n")):]
		if strings.Count(trailing, "\n") > 1 {
			chunk += "\n"
		}
		if i == 0 && gd.Doc != nil && !gd.Lparen.IsValid() {
			chunk = string(src[offset(gd.Doc.Pos()):offset(gd.Doc.End())]) +
				"\n" + chunk
		}
		chunks = append(chunks, chunk)
		start = stop
	}
	return chunks
}

func isTypeAssertion(vs *ast.ValueSpec) bool {
	if vs.Type == nil || len(vs.Values) == 0 {
		return false
	}
	for _, name := range vs.Names {
		if name.Name != "_" {
			return false
		}
	}
	return true
}

// inertAssertion recognizes conventional compile-time interface checks. Other
// forms may initialize dependencies or execute code and retain their position.
func inertAssertion(vs *ast.ValueSpec) bool {
	for _, value := range vs.Values {
		if unary, ok := value.(*ast.UnaryExpr); ok &&
			unary.Op == token.AND {

			value = unary.X
		}
		switch x := value.(type) {
		case *ast.BasicLit:
			continue

		case *ast.Ident:
			if x.Obj == nil || x.Obj.Kind != ast.Con {
				return false
			}

		case *ast.CompositeLit:
			if len(x.Elts) != 0 {
				return false
			}

		case *ast.CallExpr:
			if len(x.Args) != 1 {
				return false
			}
			arg, ok := x.Args[0].(*ast.Ident)
			if !ok || arg.Name != "nil" || arg.Obj != nil {
				return false
			}
			fun := x.Fun
			for {
				paren, ok := fun.(*ast.ParenExpr)
				if !ok {
					break
				}
				fun = paren.X
			}
			pointer, ok := fun.(*ast.StarExpr)
			if !ok || !localType(pointer.X) {
				return false
			}

		default:
			return false
		}
	}
	return true
}

// localType distinguishes pointer conversions from calls through a pointer to
// a function. Imported names cannot be resolved without package loading.
func localType(expr ast.Expr) bool {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Obj != nil && x.Obj.Kind == ast.Typ

	case *ast.StarExpr:
		return localType(x.X)

	case *ast.ParenExpr:
		return localType(x.X)

	case *ast.IndexExpr:
		return localType(x.X)

	case *ast.IndexListExpr:
		return localType(x.X)
	}
	return false
}

func usesIota(gd *ast.GenDecl) bool {
	found := false
	ast.Inspect(gd, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "iota" {
			found = true
		}
		return !found
	})
	return found
}

func numericType(expr ast.Expr, types map[string]ast.Expr,
	seen map[string]bool) bool {

	if p, ok := expr.(*ast.ParenExpr); ok {
		return numericType(p.X, types, seen)
	}
	id, ok := expr.(*ast.Ident)
	if !ok || seen[id.Name] {
		return false
	}
	if underlying, ok := types[id.Name]; ok {
		seen[id.Name] = true
		return numericType(underlying, types, seen)
	}
	switch id.Name {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint" +
		"16", "uint32", "uint64", "uintptr", "byte", "rune", "float32", "float64", "complex64", "complex128":
		return true
	}
	return false
}

func isEnumDeclaration(gd *ast.GenDecl, previous ast.Decl,
	types map[string]ast.Expr) bool {

	td, ok := previous.(*ast.GenDecl)
	if !ok || td.Tok != token.TYPE {
		return false
	}
	candidates := make(map[string]bool)
	for _, spec := range td.Specs {
		ts := spec.(*ast.TypeSpec)
		if numericType(ts.Type, types, make(map[string]bool)) {
			candidates[ts.Name.Name] = true
		}
	}
	known := make(map[string]string)
	inherited := ""
	for _, spec := range gd.Specs {
		vs := spec.(*ast.ValueSpec)
		typ := ""
		if id, ok := vs.Type.(*ast.Ident); ok {
			typ = id.Name
		} else if vs.Type == nil && len(vs.Values) == 0 {
			typ = inherited
		} else if len(vs.Values) > 0 {
			typ = enumExpressionType(
				vs.Values[0], candidates, known,
			)
			for _, value := range vs.Values[1:] {
				if enumExpressionType(
					value, candidates, known,
				) != typ {

					return false
				}
			}
		}
		if !candidates[typ] {
			return false
		}
		inherited = typ
		for _, name := range vs.Names {
			known[name.Name] = typ
		}
	}
	return len(gd.Specs) > 0
}

func enumExpressionType(expr ast.Expr, candidates map[string]bool,
	known map[string]string) string {

	switch x := expr.(type) {
	case *ast.Ident:
		return known[x.Name]

	case *ast.ParenExpr:
		return enumExpressionType(x.X, candidates, known)

	case *ast.UnaryExpr:
		return enumExpressionType(x.X, candidates, known)

	case *ast.BinaryExpr:
		a := enumExpressionType(x.X, candidates, known)
		if a != "" {
			return a
		}
		if x.Op != token.SHL && x.Op != token.SHR {
			return enumExpressionType(x.Y, candidates, known)
		}

	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && candidates[id.Name] {
			return id.Name
		}
	}
	return ""
}
