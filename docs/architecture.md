# Architecture

## Data flow

```
src []byte ─► go/parser ─► ast.File ─┐
                                     ├─► analyse() pre-pass ─► prepInfo
                                     │
                                     ▼
                              dst Decorator
                                     │
                                     ▼
                                 dst.File ──► pipeline of passes ──► dst.File'
                                                                          │
                                                                          ▼
                                                                 decorator.Fprint
                                                                          │
                                                                          ▼
                                                        gofmt printer ──► out []byte
                                                                          │
                                                                          ▼
                                                                  R10 line-length check
                                                                          │
                                                                          ▼
                                                                       diags
```

The entry point is `format.Format(src, filename, cfg)` in
[`internal/format/format.go`](../internal/format/format.go). It parses,
runs `analyse()` while AST positions are still authoritative, decorates to
dst, iterates the pipeline, prints, then runs `checkLineLength` as a
post-render check.

## Why dst, not ast

`go/printer` doesn't expose enough control over blank lines and free-floating
comments to do the layout work we need. `dave/dst` wraps ast with explicit
decoration slots (`Before`/`After` of `None`/`NewLine`/`EmptyLine`) on each
node. Setting `someNode.Decorations().Before = dst.NewLine` is how passes
say "this node starts a new line"; `EmptyLine` means "preceded by a blank
line". The printer (via `decorator.Fprint`) is gofmt's printer, so we
inherit gofmt's tab handling, alignment, and tokenisation.

**Read the dst README before touching the printer-facing logic.** In
particular, comments attach to specific decoration slots
(`NodeDecs.Start`/`End`, plus per-node slots like `BinaryExprDecorations.X`
and `.Op`). Wrong slot = comment moves to surprising places.

## The pre-pass (`analyse()`)

dst decoration mutations clear / invalidate source positions on the
ast.File. Anything we need from the original source must be captured before
decoration runs. The pre-pass collects:

- `sourceLines [][]byte` — line-indexed visual-width source. Most passes
  use this for width / indent measurement (`visualWidth`, `lineIndentAt`,
  `sourceWidth`, `visualCol` in `util.go` and `layout.go`).
- `multilineSigs map[*ast.FuncDecl]struct{}` — funcs whose signature
  spans multiple source lines. Read by R2 (via `OriginalMultilineSigs` in
  the Context) to decide whether to clear a stale body blank.
- `stringsToSplit map[*ast.BasicLit]stringSplit` — string lits whose
  source line exceeded the limit. R9 only splits BasicLits in this set
  (touching every short string would be a regression).

After decoration, the prepInfo maps are translated to dst-keyed maps and
stored in the `Context`.

## The `Context` struct

Defined in [`internal/format/passes.go`](../internal/format/passes.go).
Every pass receives one and may read/write specific fields:

| Field | Owner | Purpose |
|---|---|---|
| `Filename`, `Config`, `FileSet`, `AstFile`, `Decorator`, `File` | format.go | Plumbing; passes read |
| `SourceLines` | analyse() | Indexed source; passes read |
| `MultilineSigs map[*dst.FuncDecl]bool` | R3 may shrink | R2 reads final state |
| `OriginalMultilineSigs map[*dst.FuncDecl]bool` | analyse(), immutable | R2 reads source state |
| `StringsToSplit map[*dst.BasicLit]stringSplit` | analyse(), immutable | R9 reads |
| `OuterHandled map[*dst.CallExpr]bool` | R4/R6/R8/R12/R16 write | R4 reads to skip |

`OuterHandled` is the most-used inter-pass channel. See
[`gotchas.md`](gotchas.md#outerhandled-flow).

## Pass interface

```go
type Pass interface {
    Name() string
    Apply(ctx *Context) []diag.Diagnostic
}
```

Passes are values (no state across runs). They walk the dst tree via
`dst.Inspect` or `dstutil.Apply` and mutate decorations / nodes. Diagnostic
warnings come out of `Apply` — autofix happens via mutation, not via diag.

Pipeline assembly is at the bottom of `passes.go` as a package-level
`pipeline []Pass` slice. The order is load-bearing — see comments inline.

## Source-positional vs dst-decoration measurement

A recurring theme. Two ways to ask "is line X over limit?":

1. **Source position**: `visualWidth(ctx.SourceLines[line-1], tab)`. Right
   for decisions made early (R3, R12, R16's first-pass joinedWidth) where
   no earlier pass has reshaped the layout. Wrong once mutations move
   content off its source line.
2. **dst decoration**: walk node decorations to detect inserted/cleared
   newlines. Right for "what will the final render look like?" — used by
   R2 (`headerMultiLine`), R9 (`countWrappedAncestors`), and R16's
   stale-break detection.

When in doubt: if your decision depends on the FINAL layout, prefer
dst-based; if it depends on what the developer wrote, prefer source-based.
Mix them carefully — every oscillation bug we've hit is some pass reading
the wrong one.

## Why gofmt alignment matters

gofmt auto-aligns consecutive single-line struct/map field assignments
(colons or `=` columns). A pass that collapses one multi-line field to
single-line can change the alignment column for the whole group, pushing
some lines from 79 to 81 cols. That's the canonical oscillation pattern;
see `gotchas.md#fragile-parents`.

## Concretely: tracing a format

To debug what happens to a file:

```bash
go build -o /tmp/goformat-bin ./cmd/goformat
/tmp/goformat-bin -d path/to/file.go     # diff, doesn't write
/tmp/goformat-bin -w path/to/file.go     # apply
```

Add a `fmt.Fprintf(os.Stderr, ...)` in a pass to trace decisions. Strip
before committing — the formatter is a CLI that prints to stdout, so noise
on stderr is OK for ad-hoc debugging but `go test` does NOT swallow it.

To bisect which pass produced an unwanted change, toggle rules off via
config:

```toml
# /tmp/cfg.toml
[rules]
func_call_wrap = false
binary_op_wrap = false
```

```bash
/tmp/goformat-bin -config /tmp/cfg.toml -d file.go
```

The full set of toggles is in `internal/config/config.go` under `Rules`.
