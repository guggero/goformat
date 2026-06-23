// Package format is the core formatter. Format parses Go source, decorates it
// to a dst tree, runs the rule pipeline (see passes.go), then prints.
// Line-length checking runs on the rendered output as a post-pass.
package format

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/diag"
)

// Format formats Go source. filename is used in error and diagnostic messages.
// A nil cfg falls back to defaults.
//
// The rule pipeline runs from source positions, so a layout decision an OUTER
// pass makes (wrapping a call, shifting a block's indent) can leave an INNER
// construct rendered at a column its own pass measured from the stale, pre-wrap
// position — occasionally yielding a line that is a few columns over the limit,
// fixed only on a second run. Rather than thread post-mutation indents through
// every pass, Format iterates the whole pipeline to a fixed point: it re-parses
// and re-runs until the output stops changing (or a small cap is hit). The
// result is therefore idempotent by construction. The conservative HARD-only
// gates keep each pass from churning already-fitting code, so iteration
// converges (no oscillation) in practice within a couple of rounds.
func Format(src []byte, filename string,
	cfg *config.Config) ([]byte, []diag.Diagnostic, error) {

	if cfg == nil {
		cfg = config.Default()
	}

	// maxFormatIterations bounds the fixed-point loop. Convergence is
	// observed within 2–3 rounds; the cap is a safety net against an
	// unforeseen oscillation (it degrades to "best effort", never hangs).
	const maxFormatIterations = 6

	cur := src
	var diags []diag.Diagnostic
	for i := 0; i < maxFormatIterations; i++ {
		out, d, err := formatOnce(cur, filename, cfg)
		if err != nil {
			return nil, nil, err
		}
		diags = d
		if bytes.Equal(out, cur) {
			return out, diags, nil
		}
		cur = out
	}
	return cur, diags, nil
}

// formatOnce runs a single pass of the rule pipeline over src.
func formatOnce(src []byte, filename string,
	cfg *config.Config) ([]byte, []diag.Diagnostic, error) {

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(
		fset, filename, src, parser.ParseComments,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", filename, err)
	}

	prep := analyse(fset, astFile, src, cfg)

	dec := decorator.NewDecorator(fset)
	dstFile, err := dec.DecorateFile(astFile)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: decorate: %w", filename, err)
	}

	ctx := &Context{
		Filename:              filename,
		Config:                cfg,
		FileSet:               fset,
		AstFile:               astFile,
		Decorator:             dec,
		File:                  dstFile,
		SourceLines:           prep.sourceLines,
		MultilineSigs:         mapFuncDecls(dec, prep.multilineSigs),
		OriginalMultilineSigs: mapFuncDecls(dec, prep.multilineSigs),
		StringsToSplit: mapStringSplits(
			dec, prep.stringsToSplit,
		),
		OuterHandled: map[*dst.CallExpr]bool{},
	}

	var diags []diag.Diagnostic
	for _, p := range pipeline {
		diags = append(diags, p.Apply(ctx)...)
	}

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, dstFile); err != nil {
		return nil, nil, fmt.Errorf("%s: print: %w", filename, err)
	}
	out := buf.Bytes()

	diags = append(diags, checkLineLength(filename, out, cfg)...)

	return out, diags, nil
}

// prepInfo is the result of source-positional analysis run before dst
// decoration. Some signals (line widths, original sig spread) need accurate
// positions, which dst clears.
type prepInfo struct {
	multilineSigs  map[*ast.FuncDecl]struct{}
	stringsToSplit map[*ast.BasicLit]stringSplit
	sourceLines    [][]byte
}

// stringSplit records what we learned about an overlong string literal during
// pre-pass: source-positional info needed to budget the split chunks. R9 may
// adjust the effective indent at apply time when an enclosing call has been
// wrapped by R4 (the string then lives on a continuation line at the call's
// indent + tab).
type stringSplit struct {
	quoteCol   int // 0-based visual column of the opening quote (source)
	lineIndent int // 0-based visual indent of the line containing the lit
	lineWidth  int // total visual width of the line containing the literal
}

func analyse(fset *token.FileSet, f *ast.File, src []byte,
	cfg *config.Config) prepInfo {

	lines := splitLines(src)
	tab := cfg.TabWidth
	if tab <= 0 {
		tab = 8
	}
	limit := cfg.LineLength
	if limit <= 0 {
		limit = 80
	}

	prep := prepInfo{
		multilineSigs:  map[*ast.FuncDecl]struct{}{},
		stringsToSplit: map[*ast.BasicLit]stringSplit{},
		sourceLines:    lines,
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			if x.Body == nil || x.Type == nil {
				return true
			}
			if !isSingleLine(fset, x.Type.Func, x.Body.Lbrace) {
				prep.multilineSigs[x] = struct{}{}
			}

		case *ast.BasicLit:
			if x.Kind != token.STRING {
				return true
			}
			startPos := fset.Position(x.Pos())
			endPos := fset.Position(x.End())
			if startPos.Line != endPos.Line {
				return true
			}
			_ = startPos
			w := sourceLineWidth(fset, lines, x.Pos(), tab)
			if w > limit {
				prep.stringsToSplit[x] = stringSplit{
					quoteCol: visualCol(
						fset, lines, x.Pos(), tab,
					),
					lineIndent: lineIndentAt(
						fset, lines, x.Pos(), tab,
					),
					lineWidth: w,
				}
			}
		}
		return true
	})

	return prep
}

func mapFuncDecls(dec *decorator.Decorator,
	src map[*ast.FuncDecl]struct{}) map[*dst.FuncDecl]bool {

	out := make(map[*dst.FuncDecl]bool, len(src))
	for n := range src {
		if dn, ok := dec.Dst.Nodes[n]; ok {
			if cast, ok := dn.(*dst.FuncDecl); ok {
				out[cast] = true
			}
		}
	}
	return out
}

func mapStringSplits(dec *decorator.Decorator,
	src map[*ast.BasicLit]stringSplit) map[*dst.BasicLit]stringSplit {

	out := make(map[*dst.BasicLit]stringSplit, len(src))
	for n, info := range src {
		if dn, ok := dec.Dst.Nodes[n]; ok {
			if cast, ok := dn.(*dst.BasicLit); ok {
				out[cast] = info
			}
		}
	}
	return out
}
