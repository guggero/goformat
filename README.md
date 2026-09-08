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

## Format Git changes

Use either of these mutually exclusive flags to select edits in Git:

```sh
# Staged and unstaged changes, compared with HEAD:
goformat --uncommitted-only -w .

# Unstaged changes, compared with the index:
goformat --unstaged-only -w .

# Restrict the selection to a directory or individual files:
goformat --uncommitted-only -w accounts brokers/dfx/api.go

# Check before committing, without writing:
goformat --uncommitted-only -check .
```

With no paths, selection is limited to the current directory and its
subdirectories. Explicit paths further filter the selected files, and can refer
to another Git working tree. Directory exclusions work as usual. Untracked,
non-ignored Go files are treated as entirely new; deleted files are skipped.
Renaming a tracked file alone does not select its committed contents. Repositories
without a first commit are supported.

The formatter parses the complete working-tree file for context, then formats
the statements, signatures, fields, and comment groups affected by the changes.
A complete fix can extend beyond the original hunk, such as rewrapping the rest
of a call. Editing a package constant or variable can organize that declaration
section at the top of the file, preserving initialization order and the usual
assertion, enum, and `//noformat` exceptions. Unrelated function bodies and
statements retain their original bytes.

These flags use the existing `-d`, `-l`, `-w`, and `-check` modes and work with
`--optimize`. They require Git and cannot read stdin. The index is never rewritten:
`-w` changes the working tree, including for staged files. Review and stage the
formatting changes before committing; `-check` is suitable for a hook that should
stop until that is done. `--uncommitted-only` considers net working-tree changes
relative to `HEAD`, including both staged and unstaged edits.

Local formatting is re-parsed and checked for equivalent syntax and comment
contents. If a fix cannot be isolated safely from unrelated code, it is skipped
with a diagnostic; `-check` exits nonzero in that case. There is no fallback to
formatting the entire file.

## Rules

`goformat -rules` lists them; `goformat -explain R7` shows details.

Use `--rule` to check or fix only the listed rules:

```sh
goformat --rule R15 -check .
goformat --rule R3,R4 -w .
goformat --rule R3 --rule R4 --uncommitted-only -w .
```

Comma-separated and repeated arguments combine into one selection. IDs R1–R16
are case-insensitive; unknown or empty IDs are errors. The selection overrides
all config rule toggles: selected rules are enabled and all others disabled.
Other settings, including line width, exclusions, and `--optimize`, still apply.
The usual Go printer normalization still applies when running formatting rules;
`//noformat` and `//nolint` protections remain active.

R5 (formatting calls) and R6 (symmetry) can run independently of R4. R4 alone
preserves existing symmetric layouts but does not produce new ones. Selecting
R8 or R10 also prints their lint diagnostics to stderr, and `-check` fails on
those diagnostics. R10 alone checks the original source without rewriting it,
even with `-w`; it has no automatic fix. With Git selection flags, diagnostics
are limited to the affected constructs.

Partially wrapped function calls are corrected even when every line fits:
arguments move below the opening `(` and the closing `)` gets its own line,
unless the call uses the indentation symmetry exception. `--optimize` also
compacts already-valid layouts. Formatting and structured-log calls retain
their compact layout exceptions.

| ID  | Rule                                                              | Notes                                        |
|-----|-------------------------------------------------------------------|----------------------------------------------|
| R1  | Blank line between switch / select case clauses                   |                                              |
| R2  | Blank line after multi-line header (func / if / for / switch / range / FuncLit) | Also removes the blank when R3 collapses a sig back to single-line |
| R3  | Wrap/repack function definitions                                  | Greedy pack parameters and return types     |
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
| R14 | Collect package constants, then variables, into blocks after imports | Keeps numeric enums and interface assertions in place; preserves initialization order |
| R15 | Comment reflow: split overlong `//` comments at word boundaries   | Skips tool directives (`//go:`, `//nolint:`, `//line`), block comments, and comments with no internal spaces (URLs, dividers) |
| R16 | Break long binary expressions after operators                    | Runs before call wrapping |

## Preserve code with `//noformat`

A standalone `//noformat` (or `// noformat`) preserves the immediately following
physical line byte for byte, including whitespace. If that line opens a
multi-line construct, protection extends through its closing delimiters. This
covers functions, calls with closures, composite literals, and declaration
blocks. Put the directive immediately above the code, after any Godoc comment:

```go
// Lookup uses a deliberately aligned table.
//noformat
func Lookup(code int) string {
    return table[ code ]
}
```

Protected code is exempt from every rule, declaration movement, and line-length
diagnostics, in both default and `--optimize` modes. Other code still formats.
The existing `//nolint` behavior is unchanged.

Package-level constants and variables are collected by default into `const`
blocks followed by `var` blocks after imports, even for a single declaration.
Comments on standalone declarations move inside the collected blocks and reflow
at their new indentation. Existing blocks with headers remain separate blocks,
with their headers above them. Local declarations retain their scope. Typed blank-variable assertions stay in place,
as do const declarations immediately following a numeric type definition whose
values have that type. Numeric aliases and explicitly typed conversions are
recognized when their types can be resolved within the file.

Independent `iota` groups remain separate blocks to preserve constant values.
Variable specification order is preserved. Protected variables and assertions
with potentially effectful initializers act as ordering boundaries; later
variables collect after that boundary. A mixed block containing such an
assertion stays together. Set `[rules] declaration_grouping = false` to disable
collection.

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

- **R3 complex signatures** — signatures containing comments or multi-line
  parameter/result types retain their layout. Signatures that cannot fit at
  legal parameter/result boundaries can still exceed the limit.
- **R8 typed detection** — name-based today. `go/packages` resolution
  (so we recognise actual `btclog.Logger.*S` calls and reject false
  positives) is a future refinement.
- **R13 scope** — only `var` specifications without initializers, including
  specifications inside collected blocks. const/type declarations are out of scope.
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

## Verify a formatting commit range

Compare the combined change between two committed snapshots:

```sh
# Verify the net effect of the last three commits:
goformat -verify-diff HEAD~3..HEAD

# Reversing the endpoints reverses the diff, with the same verdict:
goformat -verify-diff HEAD..HEAD~3

# Branches, tags, and commit IDs are also accepted:
goformat -verify-diff main..formatting-branch
```

Exit **0 with empty output** means every changed file has equivalent Go syntax
and comments under the comparison rules below. Exit **1** prints differences
requiring review. Exit **2** reports an error, such as an invalid revision or
unparsable Go file, on stderr. Empty stdout is evidence of success only together
with exit 0.

The command compares the two endpoints directly, including when they are in
reverse chronological order. It checks the **net change**, not every intermediate
commit: a semantic change that was subsequently reverted is absent from the
comparison. Both endpoints must be explicit commits; `A...B` merge-base notation
is not supported.

Verification covers the entire repository, even from a subdirectory, including
`vendor`, `testdata`, and files excluded by formatter configuration. It reads Git
objects without changing the working tree, index, or history. It accepts no path
filters or other flags and does not load `goformat.toml` or run formatting passes.

For modified Go files, output is a complete unified diff of canonical AST JSON;
its line numbers refer to that representation, not the original Go source.
Formatting-only files and lines are omitted. String values are shown as escaped
Go strings so significant whitespace and non-UTF-8 bytes remain visible. Added
or deleted files, renames, mode/type changes, submodule changes, and non-Go changes
are always reported for review with their Git patches, even if their text only
differs in whitespace. No files are silently skipped.

The comparison ignores source locations, trailing commas, and literal string
splitting/joining. Decoded string bytes must match, including whitespace inside
strings. Prose comments are compared with whitespace normalized; recognized
directives and their declaration attachments are checked, and cgo preambles must
match exactly. Splitting an uninitialized variable name list into specifications
with the same types and order is accepted (R13).

This establishes structural equivalence under those rules, not arbitrary
behavioral equivalence. In particular, source-location-dependent behavior such
as `runtime.Caller` and external tools interpreting ordinary comments are outside
the claim. Declaration collection changes AST structure and declaration order;
R14 changes are conservatively reported for review.
