package format

import (
	"go/ast"
	"go/token"

	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// funcSignatureBodyBlank implements R2: when a function signature, a function
// literal header, or a control-flow header (if / for / switch / type-switch /
// select / range) spans multiple lines, the body opens with a blank line. This
// visually separates the multi-line header from the body so readers don't
// mistake the body's first statement for a continuation of the header.
//
// "Multi-line" here is judged from the FINAL output, not just the source. R4
// may wrap a call that lives inside an if-header, turning a previously
// single-line header into a multi-line one; R2 runs after R4 (see the pipeline
// in passes.go) so it sees the post-wrap state.
type funcSignatureBodyBlank struct{}

func (funcSignatureBodyBlank) Name() string { return "R2" }

func (funcSignatureBodyBlank) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.FuncSignatureBodyBlankOn() {
		return nil
	}
	dst.Inspect(ctx.File, func(n dst.Node) bool {
		switch s := n.(type) {
		case *dst.FuncDecl:
			if s.Body == nil || len(s.Body.List) == 0 {
				return true
			}
			// Clear stale blank only when R3 actually collapsed the
			// sig. A sig that was single-line in source already and
			// happens to have a deliberate body blank stays as
			// written — clearing it would inline the first body
			// comment with the `{` (dst's printer attaches Start
			// decorations to the previous token when Before=None).
			multi := ctx.MultilineSigs[s]
			origMulti := ctx.OriginalMultilineSigs[s]
			setBodyBlank(s.Body, multi, origMulti)

		case *dst.FuncLit:
			if s.Body == nil || len(s.Body.List) == 0 {
				return true
			}
			setBodyBlank(
				s.Body,
				headerMultiLine(ctx, s),
				sourceHeaderMultiLine(ctx, s),
			)

		case *dst.IfStmt, *dst.ForStmt, *dst.RangeStmt,
			*dst.SwitchStmt, *dst.TypeSwitchStmt:
			body := dstStmtBody(s.(dst.Stmt))
			if body == nil || len(body.List) == 0 {
				return true
			}
			setBodyBlank(
				body,
				headerMultiLine(ctx, s.(dst.Stmt)),
				sourceHeaderMultiLine(ctx, s.(dst.Stmt)),
			)
			// SelectStmt deliberately excluded: its "header" is just
			// "select", never multi-line.
		}
		return true
	})
	return nil
}

// setBodyBlank enforces R2 in both directions, but only when the change is
// driven by a header layout change — not when the developer wrote a blank
// they want to keep:
//
//   - finalMulti=true  → body's first statement gets a leading blank line.
//   - finalMulti=false AND sourceMulti=true → the blank is stale (R3 or
//     R16 collapsed a multi-line header), clear it.
//   - finalMulti=false AND sourceMulti=false → header was always single-
//     line; any existing blank is the developer's deliberate stanza
//     separator, leave it alone. (btcd psbt/finalizer.go has
//     `switch {\n\n\t// comment\n\tcase ...` — the blank reads as a
//     stanza header and must survive.)
func setBodyBlank(body *dst.BlockStmt, finalMulti, sourceMulti bool) {
	first := body.List[0]
	decs := first.Decorations()
	if decs == nil {
		return
	}
	if finalMulti {
		decs.Before = dst.EmptyLine
		return
	}
	if sourceMulti && decs.Before == dst.EmptyLine {
		decs.Before = dst.None
	}
}

// sourceHeaderMultiLine reports whether the header span in the SOURCE
// (keyword through body lbrace) was multi-line. Used by R2 to decide
// whether an existing body blank is a stale artifact (worth clearing
// once the final header is single-line) or a developer-intended stanza
// separator (worth keeping).
func sourceHeaderMultiLine(ctx *Context, node dst.Node) bool {
	astN, ok := ctx.Decorator.Ast.Nodes[node]
	if !ok {
		return false
	}
	start, lbrace := astHeaderSpan(astN)
	if !start.IsValid() || !lbrace.IsValid() {
		return false
	}
	return ctx.FileSet.Position(start).Line !=
		ctx.FileSet.Position(lbrace).Line
}

// headerMultiLine reports whether a function literal or control-flow
// statement's header — the span from its leading keyword to the body's
// opening "{" — spans multiple lines in the final output. The signal is
// purely dst-based: any NewLine/EmptyLine decoration inside a header part
// makes the header multi-line. A source-positional check would be wrong
// after R16 rejoins an over-split operator break — source says multi-line
// but the rendered output is single-line.
func headerMultiLine(ctx *Context, node dst.Node) bool {
	_ = ctx
	for _, part := range dstHeaderParts(node) {
		if subtreeHasNewLineDec(part) {
			return true
		}
	}
	return false
}

// astHeaderSpan returns the start (first keyword) and the body's "{" position
// for nodes whose layout follows the "header + body" shape. Both positions are
// on the same line iff the header is single-line in source.
func astHeaderSpan(node ast.Node) (start, lbrace token.Pos) {
	switch s := node.(type) {
	case *ast.IfStmt:
		if s.Body != nil {
			return s.If, s.Body.Lbrace
		}

	case *ast.ForStmt:
		if s.Body != nil {
			return s.For, s.Body.Lbrace
		}

	case *ast.RangeStmt:
		if s.Body != nil {
			return s.For, s.Body.Lbrace
		}

	case *ast.SwitchStmt:
		if s.Body != nil {
			return s.Switch, s.Body.Lbrace
		}

	case *ast.TypeSwitchStmt:
		if s.Body != nil {
			return s.Switch, s.Body.Lbrace
		}

	case *ast.SelectStmt:
		if s.Body != nil {
			return s.Select, s.Body.Lbrace
		}

	case *ast.FuncLit:
		if s.Body != nil && s.Type != nil {
			return s.Type.Func, s.Body.Lbrace
		}
	}
	return token.NoPos, token.NoPos
}

// dstHeaderParts returns the header sub-expressions/statements of a dst
// control-flow statement or function literal — the pieces we inspect for
// NewLine decorations to detect a header that became multi-line via a
// downstream pass. Body parts are excluded.
func dstHeaderParts(node dst.Node) []dst.Node {
	var parts []dst.Node
	add := func(n dst.Node) {
		if n != nil {
			parts = append(parts, n)
		}
	}
	switch s := node.(type) {
	case *dst.IfStmt:
		if s.Init != nil {
			add(s.Init)
		}
		add(s.Cond)

	case *dst.ForStmt:
		if s.Init != nil {
			add(s.Init)
		}
		if s.Cond != nil {
			add(s.Cond)
		}
		if s.Post != nil {
			add(s.Post)
		}

	case *dst.RangeStmt:
		if s.Key != nil {
			add(s.Key)
		}
		if s.Value != nil {
			add(s.Value)
		}
		add(s.X)

	case *dst.SwitchStmt:
		if s.Init != nil {
			add(s.Init)
		}
		if s.Tag != nil {
			add(s.Tag)
		}

	case *dst.TypeSwitchStmt:
		if s.Init != nil {
			add(s.Init)
		}
		if s.Assign != nil {
			add(s.Assign)
		}

	case *dst.FuncLit:
		if s.Type != nil {
			if s.Type.Params != nil {
				add(s.Type.Params)
			}
			if s.Type.Results != nil {
				add(s.Type.Results)
			}
		}
	}
	return parts
}

// dstStmtBody returns the body block of a control-flow statement.
func dstStmtBody(stmt dst.Stmt) *dst.BlockStmt {
	switch s := stmt.(type) {
	case *dst.IfStmt:
		return s.Body

	case *dst.ForStmt:
		return s.Body

	case *dst.RangeStmt:
		return s.Body

	case *dst.SwitchStmt:
		return s.Body

	case *dst.TypeSwitchStmt:
		return s.Body

	case *dst.SelectStmt:
		return s.Body
	}
	return nil
}

// subtreeHasNewLineDec walks n's subtree looking for any node carrying a
// NewLine / EmptyLine in its Before or After decoration. Used by
// headerMultiLine to detect headers that became multi-line because R4 (or some
// other pass) wrapped a call within them.
func subtreeHasNewLineDec(n dst.Node) bool {
	found := false
	dst.Inspect(n, func(m dst.Node) bool {
		if found {
			return false
		}
		if m == nil || m == n {
			return true
		}
		decs := m.Decorations()
		if decs == nil {
			return true
		}
		if decs.Before == dst.NewLine || decs.Before == dst.EmptyLine ||
			decs.After == dst.NewLine ||
			decs.After == dst.EmptyLine {

			found = true
			return false
		}
		return true
	})
	return found
}
