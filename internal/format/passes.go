package format

import (
	"go/ast"
	"go/token"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/diag"
)

// Pass is one formatter rule. Apply walks ctx.File, may mutate decorations and
// layout, and returns diagnostics for issues it can't auto-fix.
type Pass interface {
	Name() string
	Apply(ctx *Context) []diag.Diagnostic
}

// Context carries per-file inputs and pre-pass analysis shared across passes.
// Computed once in Format and threaded through every pass.
type Context struct {
	Filename  string
	Config    *config.Config
	FileSet   *token.FileSet
	AstFile   *ast.File
	Decorator *decorator.Decorator
	File      *dst.File

	// SourceLines is the input source split on '\n'. Layout passes consult
	// it for line widths and column positions.
	SourceLines [][]byte

	// MultilineSigs tracks the *final* layout decision per FuncDecl: true
	// if its signature ends up spanning multiple lines, absent or false
	// otherwise. R3 may update it (collapsing a multi-line input to
	// single-line removes the entry); R2 consults the final state.
	MultilineSigs map[*dst.FuncDecl]bool

	// OriginalMultilineSigs records the source-state signature
	// multi-line-ness, unchanged by R3. R2 needs both: it only clears a
	// body blank when R3 actually collapsed a sig (was-multi AND
	// not-currently-multi). A sig that was always single-line in source
	// with a deliberate body blank stays as the developer wrote it.
	OriginalMultilineSigs map[*dst.FuncDecl]bool

	// StringsToSplit records overlong string literals to break with `+`.
	// Drives R9.
	StringsToSplit map[*dst.BasicLit]stringSplit

	// OuterHandled records CallExprs whose layout has been chosen by an
	// outer rule (R8 placing it on its own continuation line, R4 wrapping
	// its parent). The call walker uses source-column measurement, which is
	// wrong after an outer wrap moves the call to a new column — so the
	// inner call must be skipped to avoid over-wrapping.
	OuterHandled map[*dst.CallExpr]bool
}

// pipeline is the ordered list of AST passes. R10 is a post-render check
// handled directly in Format and isn't part of this list.
//
// Ordering rationale:
//   - R1 (switch spacing) only flips Decs on case clauses; runs first.
//   - R9 (string-literal split) replaces a *dst.BasicLit with a BinaryExpr.
//     Running it before R4/R5 means R4 sees the BinaryExpr in arg position
//     and can lay it out coherently.
//   - R3 (func def wrap) may mark previously-single-line signatures as
//     multi-line by adding entries to ctx.MultilineSigs. R2 must run
//     after R3 so it picks those up.
//   - R4 (func call wrap) is independent and runs last.
var pipeline = []Pass{
	switchCaseSpacing{},
	funcDefWrap{},
	bodySplit{},
	// R7 runs before R4 so the call-wrap pass sees R7-reflowed composites
	// as multi-line containers and can apply the inline-symmetric form
	// (`f(a, &T{ ... })`).
	compositeLitReflow{},
	// R8 runs before R4 so it can mark inner calls as OuterHandled and
	// apply its own structured-log layout.
	structuredLogWrap{},
	// R16 runs before R4 so its operator-break decision marks operand calls
	// as OuterHandled — preventing R4 from re-wrapping a call whose
	// containing binary expression has just been broken at the operator.
	binaryOpWrap{},
	funcCallWrap{},
	// R9 runs after R4 so it sees the final wrap state and budgets the
	// chunks against the post-wrap indent (a string arg of a wrapped call
	// lives one tab deeper than its source column). R9 also reflows
	// existing string-concat chains — joining and re-splitting at optimal
	// positions — so it subsumes what the old separate "string join" pass
	// used to do.
	stringLitWrap{},
	varBlockWrap{},
	// R2 runs after R3/R4 so it sees the FINAL multi-line state of every
	// function signature, control-flow header, and func literal. Wrapping a
	// call inside an if/for/switch header turns that header multi-line in
	// the output — even if it was single-line in source — and R2 needs
	// to honour that to add the body blank.
	funcSignatureBodyBlank{},
	// Stanza spacing runs last so it sees the final block layout.
	stanzaSpacing{},
	// Comment reflow is purely textual on the dst decoration slots — it
	// doesn't depend on any other pass's output, so it goes at the very
	// end.
	commentReflow{},
}
