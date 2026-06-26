package format

import (
	"github.com/dave/dst"

	"github.com/guggero/goformat/internal/diag"
)

// stanzaSpacing implements R11: inside a block, any statement (after the first)
// that has a leading comment gets a blank line before it. This is the doc's
// "logical stanzas" rule, restricted to the conservative signal we can detect
// — a comment introducing a new section. We never invent a blank line where
// the source has no comment; we only enforce the case where the developer
// already marked a new stanza.
//
// The first statement in a block is exempt: it's right after `{` and R2 owns
// the body-blank rule for multi-line signatures.
type stanzaSpacing struct{}

func (stanzaSpacing) Name() string { return "R11" }

func (stanzaSpacing) Apply(ctx *Context) []diag.Diagnostic {
	if !ctx.Config.Rules.StanzaSpacingOn() {
		return nil
	}
	dst.Inspect(ctx.File, func(n dst.Node) bool {
		if ctx.SkipNolintDecl(n) {
			return false
		}
		block, ok := n.(*dst.BlockStmt)
		if !ok || len(block.List) < 2 {
			return true
		}
		for i := 1; i < len(block.List); i++ {
			stmt := block.List[i]
			decs := stmt.Decorations()
			if decs == nil {
				continue
			}
			if len(decs.Start) == 0 {
				continue
			}
			decs.Before = dst.EmptyLine
		}
		return true
	})
	return nil
}
