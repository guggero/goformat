# goformat — agent entrypoint

`goformat` is a configurable Go formatter that layers lnd-style guidelines on
top of `gofmt`. It parses Go source, decorates it via `dave/dst`, runs a
pipeline of layout passes, then prints with gofmt's printer. Stronger than
`gofmt` on: function-call wrapping, switch-case spacing, structured-log calls,
string-literal reflow, composite-literal layout, comment reflow, binary-op
breaks, and a few more.

The user is **Oli**, a deep-Go engineer working on Lightning Network code
(lnd / btcd / taproot-assets). When editing Go in this repo OR consuming this
tool's output, follow the [`go-backend-style`](#) skill. Real-world test
benches: `../../go/src/github.com/lightningnetwork/lnd` and
`../../go/src/github.com/btcsuite/btcd` (psbt package).

## Where to read first

| If you're … | Start with |
|---|---|
| Brand new to the project | [`docs/architecture.md`](docs/architecture.md) — how the pipeline is wired |
| Hunting a specific rule's behaviour | [`docs/rules.md`](docs/rules.md) — per-pass reference |
| Debugging a regression / oscillation | [`docs/gotchas.md`](docs/gotchas.md) — every pitfall we've hit |
| Adding/modifying a test | [`docs/testing.md`](docs/testing.md) — testdata conventions |

`README.md` is the user-facing intro; the four files above are the
engineer-facing companion.

## File layout

```
cmd/goformat/           CLI entry (flag parsing, stdin/-d/-w/-check modes)
internal/config/        TOML config + Rules struct
internal/diag/          Diagnostic type used by passes
internal/format/        The actual formatter
  format.go             Format() entry, analyse() pre-pass, Context shape
  passes.go             Pass interface + ORDERED pipeline + Context fields
  rules.go              RuleDoc registry (drives -explain)
  layout.go             sourceWidth / argWidths / packLayout shared helpers
  util.go               visualWidth, lineIndentAt, isSingleLine, …
  pass_*.go             One file per rule (R1, R2, R3, …)
testdata/               R<N>_<name>/<case>.in.go + .out.go pairs
```

## Common workflows

```bash
# Build + run on a file
go build -o /tmp/goformat-bin ./cmd/goformat
/tmp/goformat-bin -w path/to/file.go

# Full test suite (testdata pairs + line-length + strlog units)
go test ./...

# Idempotency check (formatter should be a fixed point)
cp -r internal/format /tmp/_before
/tmp/goformat-bin -w internal/format/*.go
diff -r /tmp/_before internal/format    # should print nothing

# Eat your own dogfood on the formatter source itself
/tmp/goformat-bin -d internal/format/*.go

# Real-world bench (instructive when changing layout passes)
cp ../../go/src/github.com/btcsuite/btcd/psbt/finalizer.go /tmp/f.go
/tmp/goformat-bin -d /tmp/f.go
```

## Three guiding principles

Re-read these before any change to a layout pass:

1. **Don't churn what already fits.** The 80-col limit is a ceiling, not a
   target. If a multi-line layout has every line ≤ limit AND it sits in a
   layout-fragile context (struct literal, operator-split binary), leave it
   alone. Re-flowing breaks stability — see
   [`docs/gotchas.md#oscillation`](docs/gotchas.md#oscillation).
2. **Source positions are stale after passes mutate dst.** Anything a later
   pass needs from the pre-mutation source must be captured in `analyse()`
   (`SourceLines`, `MultilineSigs`, `OriginalMultilineSigs`, `StringsToSplit`)
   or recomputed from current dst decorations.
3. **Inter-pass contract = `OuterHandled` set.** When an outer pass commits to
   a layout for some `*dst.CallExpr`, it adds the call to
   `ctx.OuterHandled`; R4 then skips it. Forgetting this is the source of
   most over-wrapping bugs.

## Quick pipeline reference

In `internal/format/passes.go`, in execution order:

```
R1  switchCaseSpacing      blank line between case clauses
R3  funcDefWrap            wrap overlong func sigs
R12 bodySplit              split `func F() T { stmt }` when over limit
R7  compositeLitReflow     struct/slice composite reflow
R8  structuredLogWrap      log.InfoS(ctx, "msg", attrs...) layout
R16 binaryOpWrap           &&/||/+ chain break BEFORE R4 wraps inner calls
R4  funcCallWrap           pack-or-spread; includes R5 (fmt funcs) + R6 (symmetric)
R9  stringLitWrap          string-literal join + multi-split
R13 varBlockWrap           long `var a,b,c T` → `var ( ... )` block
R2  funcSignatureBodyBlank blank under multi-line header
R11 stanzaSpacing          blank above comment-led stmt
R15 commentReflow          reflow `//` comments that exceed limit
                           ──── then post-render ────
R10 linelen check          surface any remaining over-limit line as a diag
```

The "why this order" is encoded in passes.go comments — read them before
reshuffling.
