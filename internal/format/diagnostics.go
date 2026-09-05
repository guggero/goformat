package format

import (
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/diag"
)

// Diagnostics checks enabled lint rules without formatting. Positions refer to
// src, which may contain intentionally unformatted code outside changed hunks.
func Diagnostics(src []byte, filename string,
	cfg *config.Config) ([]diag.Diagnostic, error) {

	if cfg == nil {
		cfg = config.Default()
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var diagnostics []diag.Diagnostic
	if cfg.Rules.StructuredLogWrapOn() {
		protected := noformatRanges(src)
		ctx := &Context{FileSet: fset, Filename: filename}
		ast.Inspect(file, func(n ast.Node) bool {
			if fd, ok := n.(*ast.FuncDecl); ok &&
				hasNolint(fd.Doc) {

				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			start := fset.Position(call.Pos()).Offset
			end := fset.Position(call.End()).Offset
			for _, span := range protected {
				if start < span.end && end > span.start {
					return false
				}
			}
			if isStructuredLogCall(call, cfg.StructuredLogMethods) {
				if d := lintStructuredLogMsg(
					ctx, call,
				); d != nil {

					diagnostics = append(diagnostics, *d)
				}
			}
			return true
		})
	}
	return append(diagnostics, outputLineDiagnostics(
		src, filename, cfg,
	)...), nil
}
