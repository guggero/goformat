// Package syntax compares source structure while allowing formatting changes.
package syntax

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
)

var (
	posType = reflect.TypeOf(token.NoPos)
)

// Fingerprint ignores source positions and comment wrapping, and folds only
// literal string concatenations. It does not evaluate general Go expressions.
func Fingerprint(src []byte) ([]byte, error) {
	f, err := parser.ParseFile(
		token.NewFileSet(), "input.go", src,
		parser.ParseComments|parser.SkipObjectResolution,
	)
	if err != nil {
		return nil, err
	}
	var words, directives, cgo []string
	for _, group := range f.Comments {
		for _, comment := range group.List {
			text := comment.Text
			body := strings.TrimSpace(strings.TrimPrefix(
				text, "//",
			))
			if isDirective(text) {

				// Keep directives exact and distinguish
				// build-tag placement before versus after the
				// package clause.
				directives = append(directives, fmt.Sprintf(
					"%t:%s", comment.Pos() < f.Package,
					text,
				))
			}
			body = strings.TrimSuffix(
				strings.TrimPrefix(body, "/*"), "*/",
			)
			words = append(words, strings.Fields(body)...)
		}
	}

	// A cgo preamble contains C code, so preserve it byte-for-byte.
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gd.Specs {
			im := spec.(*ast.ImportSpec)
			if value, _ := strconv.Unquote(
				im.Path.Value,
			); value != "C" {

				continue
			}
			for _, group := range []*ast.CommentGroup{
				gd.Doc, im.Doc,
			} {

				if group != nil {
					for _, comment := range group.List {
						cgo = append(cgo, comment.Text)
					}
				}
			}
		}
	}
	return json.Marshal([]any{
		canonical(reflect.ValueOf(f)), words, directives, cgo,
	})
}

func isDirective(text string) bool {
	body := strings.TrimSpace(strings.TrimPrefix(text, "//"))
	return strings.HasPrefix(body, "go:") ||
		strings.HasPrefix(body, "+build") ||
		strings.HasPrefix(body, "line ") ||
		strings.HasPrefix(body, "export ") ||
		strings.HasPrefix(body, "nolint") ||
		strings.HasPrefix(body, "noformat") ||
		strings.HasPrefix(text, "/*line ")
}

func canonical(v reflect.Value) any {
	if !v.IsValid() || ((v.Kind() == reflect.Pointer ||
		v.Kind() == reflect.Interface) && v.IsNil()) {

		return nil
	}
	if v.CanInterface() {
		if decl, ok := v.Interface().(*ast.GenDecl); ok &&
			decl.Tok == token.VAR {

			// R13 splits uninitialized name lists across
			// specifications. Preserve declaration order and types
			// while ignoring only that grouping.
			copy := *decl
			copy.Specs = nil
			expanded := false
			for _, spec := range decl.Specs {
				value := spec.(*ast.ValueSpec)
				if len(value.Values) != 0 ||
					len(value.Names) < 2 {

					copy.Specs = append(copy.Specs, spec)
					continue
				}
				expanded = true
				for i, name := range value.Names {
					item := *value
					item.Names = []*ast.Ident{name}
					if i > 0 {
						item.Doc = nil
					}
					if i < len(value.Names)-1 {
						item.Comment = nil
					}
					copy.Specs = append(copy.Specs, &item)
				}
			}
			if expanded {
				return canonical(reflect.ValueOf(copy))
			}
		}
		if expr, ok := v.Interface().(ast.Expr); ok {
			if value, ok := stringValue(expr); ok {
				// Encode bytes explicitly: JSON would otherwise
				// replace invalid UTF-8 and hide string
				// changes.
				return []any{"string", []byte(value)}
			}
		}
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return canonical(v.Elem())

	case reflect.Struct:
		result := map[string]any{"type": v.Type().String()}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			value := v.Field(i)
			if field.Type == posType {
				// These optional tokens change program meaning:
				// f(xs...) versus f(xs), and type T = U versus
				// T U.
				if field.Name == "Ellipsis" ||
					field.Name == "Assign" {

					result[field.Name] = value.Int() != 0
				}
				continue
			}
			switch field.Name {
			case "Doc", "Comment":
				// Preserve attachment of directives to
				// declarations, e.g. moving go:embed to another
				// variable matters.
				group, _ := value.Interface().(*ast.CommentGroup)
				var attached []string
				if group != nil {
					for _, comment := range group.List {
						if isDirective(comment.Text) {
							attached = append(
								attached,
								comment.Text,
							)
						}
					}
				}
				if len(attached) > 0 {
					result[field.Name] = attached
				}
				continue

			case "Comments", "Scope", "Obj", "Unresolved":
				continue
			}
			result[field.Name] = canonical(value)
		}
		return result

	case reflect.Slice:
		result := make([]any, v.Len())
		for i := range result {
			result[i] = canonical(v.Index(i))
		}
		return result
	}
	return v.Interface()
}

func stringValue(expr ast.Expr) (string, bool) {
	switch x := expr.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			value, err := strconv.Unquote(x.Value)
			return value, err == nil
		}

	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			left, lok := stringValue(x.X)
			right, rok := stringValue(x.Y)
			return left + right, lok && rok
		}
	}
	return "", false
}
