# Gotchas — every pitfall we've hit, indexed for grep

The whole point of this file is for the next agent to grep for symptoms
before re-discovering the same bug.

## OuterHandled flow

The single most important inter-pass channel. `ctx.OuterHandled` is a
`map[*dst.CallExpr]bool` populated by outer passes to tell R4 "I've
already chosen this call's layout, don't touch it." R4 checks
`if ctx.OuterHandled[call] { return true }` at the top of its loop.

**Who marks what:**

| Pass | When | Helper |
|---|---|---|
| R4 (layoutPack) | Wrapping a call in pack form | `markInnerCallsHandled` |
| R4 (layoutSymmetric) | Symmetric form, container can be FuncLit | `markInnerCallsHandledDeep` |
| R8 (structuredLogWrap) | Owning a log call | `markInnerCallsHandled` |
| R12 (bodySplit) | After splitting a single-line body | local dst.Inspect loop |
| R16 (binaryOpWrap) | After breaking a binary chain at ≥ 1 operator | `markBinaryInnerCallsHandled` |

**`markInnerCallsHandled` vs `markInnerCallsHandledDeep`:**

```go
// markInnerCallsHandled: STOPS at FuncLit boundary.
// Use when inner calls have their own indentation scope (closures), and
// their wrap decisions should still be R4's job.
//   t.Run("...", func() { chainhash.NewHashFromStr(...) })
//                         ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^ should still be
//                                                       wrappable by R4

// markInnerCallsHandledDeep: DESCENDS into FuncLit bodies.
// Use when the outer layout has shifted the closure body's effective
// indent — R4 measuring from source columns would over-wrap.
//   dstutil.Apply(file, func(c){ ... }, nil)  <-- symmetric form
//   shifts closure body 1 tab shallower than source.
```

If R4 is over-wrapping inside a closure body of a symmetric-form call,
suspect `markInnerCallsHandled` where `markInnerCallsHandledDeep` is
needed.

## Oscillation

The formatter is supposed to be idempotent — `format(format(x)) ==
format(x)`. Every layout pass that re-flows ALREADY-FITTING content risks
oscillation. Three patterns we hit:

### gofmt alignment padding (struct literals)

A `KeyValueExpr` inside a `CompositeLit` whose value is multi-line breaks
gofmt's alignment of consecutive keyed fields. Collapsing it to single-
line lets gofmt insert padding spaces that push the line over the limit;
next run wraps it again. Cycle.

**Fix:** `insideLayoutFragileParent(parents, call)` returns true when an
ancestor is `KeyValueExpr` → `CompositeLit`. R4 and R9 skip reflow when
source is multi-line + all lines fit + fragile parent.

### Operator-split binary expressions

`A + B + C + D` with R16 breaking at one operator. R4 walks each operand
call independently; per-call projected width misses sibling content on
adjacent lines, so R4 may collapse a multi-line call. The next run sees
the now-overlong composite line and re-wraps. Cycle.

**Fix:** Same `insideLayoutFragileParent`. The BinaryExpr branch returns
true when `subtreeHasNewLineDec(parent)` — any operator break in the
subtree means the call sits in a layout that R4 can't reason about
locally.

### Indent shift after symmetric layout

When R4 makes `dstutil.Apply(file, func(){...}, nil)` symmetric, the
closure body's effective indent drops by one tab. Inner calls measured
against source columns appear to overflow when they actually fit. R4
over-wraps. Next run sees the wraps and may collapse. Cycle.

**Fix:** `markInnerCallsHandledDeep` for the symmetric path. R10 still
flags any line that's genuinely over after the shift.

### Verifying idempotency

```bash
cp -r internal/format /tmp/_a
/tmp/goformat-bin -w internal/format/*.go
/tmp/goformat-bin -w internal/format/*.go    # second run
cp -r internal/format /tmp/_b
diff -r /tmp/_a /tmp/_b    # MUST be empty after first run
```

The btcd psbt directory and lnd are good real-world stress benches.

## Fragile parents

The concept threaded through R4, R9, R16. A "fragile parent" is one whose
final rendering depends on whether children are single- or multi-line.
The helper `insideLayoutFragileParent(parents, node)` in
`pass_funccall.go` defines the contract:

```go
case *dst.KeyValueExpr:
    // parent of KeyValueExpr is the CompositeLit — alignment risk
    if _, isComp := parents[parent].(*dst.CompositeLit); isComp {
        return true
    }
case *dst.BinaryExpr:
    if subtreeHasNewLineDec(parent) {
        return true
    }
```

If you're considering re-flowing a fitting multi-line layout, run this
check first.

## Deliberate blanks (R2)

Bug we hit on btcd's `psbt/finalizer.go`:

```go
switch {

	// A witness input ...
	// relevant sigScript ...
	case pInput.WitnessUtxo != nil:
```

The blank under `switch {` is a deliberate stanza separator. R2 was
clearing it because `headerMultiLine(switch) == false` (single-line
header, no signal to wrap). The fix taught R2 the asymmetry:

```
finalMulti  sourceMulti  action
true        any          set EmptyLine
false       true         clear EmptyLine (stale: R3/R16 collapsed)
false       false        leave it (developer's deliberate blank)
```

`sourceHeaderMultiLine` reads the AST header span; `headerMultiLine`
reads dst decorations (final state).

## Stale source positions

The original sin. Once a pass mutates dst, source positions on the
underlying ast.Node are NOT updated. Two consequences:

1. **Pre-pass before mutations.** Anything you need from pristine source
   goes in `analyse()` (format.go) and is stored in `Context`.
2. **Post-mutation, prefer dst decorations.** "Is this header multi-line
   in the final output?" is a dst-decoration question, not a source-
   position question.

If you find yourself writing `ctx.FileSet.Position(node.Pos()).Line` deep
in the pipeline, ask whether that source position is still meaningful
under the layout the previous passes have committed to.

## R16 width measurement traps

Two errors we hit while building R16:

### Wrong prefix calc

Earlier code hard-coded `stmtPrefixWidth(IfStmt) = 3, ReturnStmt = 7,
ForStmt = 4, AssignStmt = 0`. AssignStmt = 0 was wrong:
`contIndent := f(...) + tab` has a 14-char prefix `contIndent := ` that
R16 ignored, so it thought operator-split would fit when line 1 actually
overflows. Bug surfaced as oscillation on `pass_complit.go` line 125.

**Right answer:** `prefixW := sourceWidth(astStmt.Pos(), astBin.Pos())`.
This works uniformly for `if `, `return `, `x := `, `if init; `, etc.

### Wrong rendered-indent calc

Earlier code used `lineIndentAt(blockBody.Lbrace) + tab`. Wrong for
multi-line function signatures:

```go
func applyFormattingCallLayout(ctx *Context, astCall *ast.CallExpr,
	call *dst.CallExpr, limit, tab int) bool {
//  ^ continuation tab here; the { is on this line, so lineIndentAt = 8
//  but the func's actual indent is 0, body indent should be 0+8 = 8.
```

So we'd compute `body indent = 8 + 8 = 16`, overcounting by 1 tab. R16
then thought joined widths were larger than they actually rendered.

**Right answer:** walk parents from BlockStmt to its OWNER (FuncDecl /
IfStmt / ForStmt), use owner's first source line indent + tab. See
`stmtRenderedIndent`.

## R16 chain coverage

R16 only handles statement-level slots: `IfStmt.Cond`, `ForStmt.Cond`,
single-Result `ReturnStmt`, single-RHS `AssignStmt`, `ExprStmt.X`. Binary
expressions in OTHER positions (function arguments, indexed expressions,
field initializers, etc.) are not directly broken at the operator.

If a user reports an over-limit binary expression that R16 doesn't fix,
check whether the slot is in the supported list before deciding it's a
bug.

## R9 string anchor column

R9 uses the leftmost lit's source column as anchor. After R16 splits an
enclosing chain, the source column is stale — the lit now lives on a
continuation line. R9 compensates via `countWrappedAncestors`, which
counts each wrapped CallExpr AND each operator-split BinaryExpr ancestor
as one tab of indent shift.

**Symptom of regression:** Strings get mid-word-split (`"//n"+"olint:"`)
when they should fit whole on a continuation line.

## R12 + body-collapsed source

When source has `func F() T { stmt }` on one over-limit line, R12 splits
it. Any pass that wants to measure that statement's "real" line width
must use the rendered indent (parent block's owner-indent + tab), NOT the
source line width — that's the over-limit collapsed-body line.

R16 explicitly checks `enclosingBlockIsSingleLine` and bails when the
enclosing block is body-collapsed in source. Without this, R16 would
over-split inside what R12 is about to break.

## R15 — limit is a ceiling

`reflowCommentBlock` only re-flows when at least one paragraph line
exceeds the budget. **Don't change this** — re-flowing fitting prose
broke stability twice (once because R6 shifted indent and R15 then
re-flowed comments at the new wider width on the next run).

R15 still doesn't track indent shifts perfectly — when R6 reshapes an
enclosing call, the comment's effective indent changes. R15 sees the new
indent and may produce different breaks. In practice this means the
formatter stabilises in ≤ 2 runs for files heavily affected by R6, not 1.
Accepted limitation.

## Comment slot assignment

A subtle dst behaviour: a `// trailing` comment on the same source line
as a token attaches to the token's `End` decoration slot. A standalone
comment on its own line attaches to the NEXT statement's `Start` slot.

If a pass clears the wrong `Before` decoration (e.g., clears
`body.List[0].Decorations().Before = dst.None` when it was `dst.NewLine`
from R12), the printer may inline the body's first comment with the
opening `{` — comment ends up on the wrong line.

Concretely: `setBodyBlank` carefully only clears `dst.EmptyLine`, never
`dst.NewLine` — because R12 uses NewLine to split a body, and undoing
that here would collapse the body back inline.

## Recovering from a bad format

If the formatter trashes a file you care about, `git checkout` the file
in the target repo. Locally during development:

```bash
# Save before formatting
cp -r internal/format /tmp/_save

# ... change a pass, run formatter ...

# Undo:
cp -r /tmp/_save/* internal/format/
```

`git stash` works too if changes are pre-tracked.

## Performance

Each pass walks the full dst tree once via `dst.Inspect`. `parents` maps
are built per-pass that need them (R4, R9, R16) via `buildDstParents`.
For files in the 100-1000 line range, total format time is dominated by
gofmt's printer. Don't micro-optimise pass walks; do collapse multiple
walks into one if you find yourself adding a third map-builder.
