# goformat

A configurable Go formatter that enforces the `lnd`-style guidelines on top of
`gofmt`. Built for codebases that want stricter wrapping, switch/case spacing,
structured-log conventions, comment & string reflow, and composite-literal
discipline that `gofmt` alone doesn't provide.

**Status:** v0.2 — used in anger on real codebases (lnd, btcd/psbt). Stable
enough for CI; corner cases still surface as needed.

## Install

```
go install github.com/guggero/goformat/cmd/goformat@latest
```

## Quick start

```
# Check (CI mode — exits non-zero on pending changes):
goformat -check ./...

# Show diffs for files that would change:
goformat -d ./...

# Apply in place:
goformat -w ./...

# Pipe through stdin (gofmt-compatible):
cat file.go | goformat
```

## Rules

`goformat -rules` lists them; `goformat -explain R7` shows details.

| ID  | Rule                                                              | Notes                                        |
|-----|-------------------------------------------------------------------|----------------------------------------------|
| R1  | Blank line between switch / select case clauses                   |                                              |
| R2  | Blank line after multi-line header (func / if / for / switch / range / FuncLit) | Also removes the blank when R3 collapses a sig back to single-line |
| R3  | Wrap/repack function definitions                                  | Greedy pack; no return-list wrapping yet     |
| R4  | Wrap/repack overlong function calls                               | Pack-or-spread; bails on multi-line method chains |
| R5  | Formatting funcs: split format string with `+`                    | Allow/deny lists configurable; preserves multi-line layouts whose lines already fit |
| R6  | Indentation symmetry for nested calls                             | Preserve AND produce both inline-symmetric (`f(a, &T{ ... })`) and wrapped-symmetric (`f(\n  a, &T{ ... },\n)`) forms |
| R7  | Composite-literal reflow                                          | Structs/maps: one element per line. Slices/arrays: greedy pack |
| R8  | Structured-log layout + static-msg lint                           | Name-based detection (TraceS / DebugS / ...) |
| R9  | String-literal reflow (split, join, re-split)                     | Multi-split, effective-indent-aware, walks dst parents for wrap context |
| R10 | Warn on lines exceeding the limit                                 | Honors `//nolint:ll`                          |
| R11 | Stanza spacing: blank line before comment-led statements          |                                              |
| R12 | Body split: single-line function body whose line exceeds limit    |                                              |
| R13 | Var-block wrap: long `var a,b,c,…T` → `var ( ... )` block         | Var only; no const/type, no value-bearing    |
| R15 | Comment reflow: split overlong `//` comments at word boundaries   | Skips tool directives (`//go:`, `//nolint:`, `//line`), block comments, and comments with no internal spaces (URLs, dividers) |

## Configuration

Drop a `goformat.toml` at the repo root. Every key is optional;
`goformat.toml.example` shows defaults.

```toml
line_length = 80
tab_width   = 8

# Calls treated as formatting-style (compact-`+` wrap when overlong).
formatting_funcs = [
  "fmt.Errorf", "fmt.Printf", "fmt.Sprintf",
  "log.Tracef", "log.Debugf", "log.Infof",
  "log.Warnf",  "log.Errorf", "log.Criticalf",
  "t.Errorf",   "t.Fatalf",
  "require.NoErrorf", "require.Errorf",
  "assert.Errorf",
  "t.Logf",
]

# Exact-match overrides — calls that LOOK like formatting funcs but
# should wrap normally instead.
formatting_funcs_deny = []

# Method names recognised as structured-log calls (R8).
structured_log_methods = [
  "TraceS", "DebugS", "InfoS", "WarnS", "ErrorS", "CriticalS",
]

[rules]
# All on by default. Flip to false to disable a specific rule.
# func_call_wrap   = false
# comment_reflow   = false
```

### Allow vs. deny semantics

`formatting_funcs` uses **trailing-segment** matching — an entry
`fmt.Errorf` matches `fmt.Errorf`, `srvrLog.Errorf`, or any bare
`Errorf` identifier. The deny list uses **exact** matching, so you can
single out e.g. `require.NoErrorf` without it accidentally rejecting
`fmt.Errorf` through the shared `Errorf` suffix.

## Design

Pipeline: `go/parser` → `dave/dst` decoration → AST passes → restore → print.
Width-driven rules (R3 / R4 / R7 / R9 / R13) run a source-position pre-pass
on the AST (which still has accurate positions), then mutate `dst`
decorations and let the printer emit properly wrapped code. R9 walks the
dst parent chain at apply time to compute post-wrap effective indents.

| Component              | Purpose                                       |
|------------------------|-----------------------------------------------|
| `cmd/goformat`         | CLI (`-w`, `-d`, `-l`, `-check`, `-explain`)  |
| `internal/config`      | TOML config + lnd-style defaults              |
| `internal/format`      | Pipeline + one file per rule (R1, R2, …)      |
| `internal/diag`        | Diagnostic type used by warn-only rules       |
| `testdata/Rn_*/`       | In/out pairs per rule (driver: `go test`)     |

Tests live alongside the code. `go test ./...` runs everything: unit
pairs, idempotency invariant (`Format(out) == out`), R8 lint checks,
R10 diagnostic checks.

## Limitations

The remaining edges are corner cases:

- **R3 single-param / return-list wrapping** — only multi-param signature
  wrapping is implemented; a sig with one very long param can still
  exceed the limit.
- **R8 typed detection** — name-based today. `go/packages` resolution
  (so we recognise actual `btclog.Logger.*S` calls and reject false
  positives) is a future refinement.
- **R13 scope** — only ungrouped `var` decls with a single ValueSpec
  and no values. const/type/multi-spec are out of scope.
- **Long-token comments** — URLs and decorative `===` dividers in
  comments have no word boundaries to split at, so R15 leaves them
  alone (R10 still warns).
- **Long boolean expressions** — chained `||` / `&&` in if-conditions
  aren't yet repacked.

`goformat -rules` and `goformat -explain Rn` reflect this in-CLI.

## Build & test

```
go test ./...                              # unit pairs + idempotency + lints
go test ./... -run TestUnitPairs -update   # regenerate golden .out.go files
go vet ./cmd/... ./internal/...
go build -o ./goformat ./cmd/goformat
```

`testdata/Rn_<rule>/<name>.in.go` pairs with `<name>.out.go`; the
harness asserts `Format(in) == out` and `Format(out) == out`
(idempotency). Add a new rule case by dropping a new `.in.go`,
running `go test … -update`, and reviewing the generated `.out.go`.
