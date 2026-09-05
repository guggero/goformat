// check-formatting compares committed Go files independently of the formatter.
// Run: go run ./scripts/check-formatting.go -repo /path/to/repo -path core
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
)

func main() {
	repo := flag.String("repo", ".", "Git repository")
	base := flag.String("base", "HEAD^", "base commit")
	head := flag.String("head", "HEAD", "formatted commit")
	path := flag.String("path", ".", "repository-relative path to check")
	flag.Parse()
	if err := check(*repo, *base, *head, *path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func git(repo string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %v: %w: %s", args, err, &stderr)
	}
	return out, nil
}

func check(repo, base, head, path string) error {
	for _, ref := range []*string{&base, &head} {
		out, err := git(
			repo, "rev-parse", "--verify", "--end-of-options",
			*ref+"^{commit}",
		)
		if err != nil {
			return err
		}
		*ref = strings.TrimSpace(string(out))
	}
	raw, err := git(
		repo, "diff", "--raw", "--no-abbrev", "--no-renames", "-z",
		base, head, "--", path,
	)
	if err != nil {
		return err
	}
	entries := bytes.Split(bytes.TrimSuffix(raw, []byte{0}), []byte{0})
	checked, failed := 0, 0
	for i := 0; i+1 < len(entries); i += 2 {
		meta := strings.Fields(string(entries[i]))
		name := string(entries[i+1])
		if len(meta) != 5 {
			return fmt.Errorf("unexpected Git diff entry: %q", entries[i])
		}
		if meta[4] != "M" ||
			strings.TrimPrefix(meta[0], ":") != meta[1] ||
			!strings.HasSuffix(name, ".go") {

			fmt.Printf("REVIEW %s: addition, deletion, mode "+
				"change, or non-Go file\n", name)
			failed++
			continue
		}
		var snapshots [2][]byte
		for j, ref := range []string{base, head} {
			src, err := git(repo, "show", ref+":"+name)
			if err != nil {
				return err
			}
			snapshots[j], err = fingerprint(src)
			if err != nil {
				return fmt.Errorf("%s at %s: %w", name, ref, err)
			}
		}
		checked++
		if !bytes.Equal(snapshots[0], snapshots[1]) {
			fmt.Printf("REVIEW %s: syntax, string value, or "+
				"comments differ\n", name)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d file(s) need review; checked %d Go "+
			"file(s)", failed, checked)
	}
	fmt.Printf("PASS: %d changed Go files have equivalent syntax and "+
		"comments\n", checked)
	return nil
}

// fingerprint ignores source positions and comment wrapping, and folds only
// literal string concatenations. It does not evaluate general Go expressions.
func fingerprint(src []byte) ([]byte, error) {
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

var posType = reflect.TypeOf(token.NoPos)

func isDirective(text string) bool {
	body := strings.TrimSpace(strings.TrimPrefix(text, "//"))
	return strings.HasPrefix(body, "go:") ||
		strings.HasPrefix(body, "+build") ||
		strings.HasPrefix(body, "line ") ||
		strings.HasPrefix(body, "export ") ||
		strings.HasPrefix(body, "nolint") ||
		strings.HasPrefix(text, "/*line ")
}

func canonical(v reflect.Value) any {
	if !v.IsValid() || ((v.Kind() == reflect.Pointer ||
		v.Kind() == reflect.Interface) && v.IsNil()) {

		return nil
	}
	if v.CanInterface() {
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
