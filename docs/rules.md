# Rules reference

One pass per rule. Numbering reflects historical order; pipeline order is
different — see the bottom of this doc and `passes.go`.

R10 is a post-render diagnostic check (not in the pass pipeline). R14 was
skipped during numbering. R6 lives inside R4 as a layout strategy, not its
own pass file.

## R1 — switch / select case spacing
**File:** `pass_switch.go`
**Touches:** `Decs` on `*dst.CaseClause` / `*dst.CommClause`.
**What:** Ensures a blank line between consecutive case clauses when the
switch / select has ≥ 2 cases. Comments attached to a case stay attached;
the blank goes above the comment.
**Order:** First. Pure decoration; independent of other passes.

## R2 — body blank after multi-line header
**File:** `pass_funcsig.go`
**Touches:** `Before` decoration on `body.List[0]` of FuncDecl / FuncLit /
control-flow statements.
**What:** When a header (func signature, func literal header, if/for/range/
switch/type-switch header) spans multiple lines in the FINAL output, the
body's first statement gets `Before = EmptyLine`.
**Critical asymmetry:** Only CLEARS a blank when source-header WAS multi-
line AND final-header IS single-line (R3 or R16 collapsed it). A deliberate
blank under an always-single-line header (`switch {\n\n\t// note\n\tcase ...`)
is preserved — see `gotchas.md#deliberate-blanks`.
**Order:** Late. Must run after R3, R4, R16 (anything that can change
header multi-line state).

## R3 — function-definition wrapping
**File:** `pass_funcdef.go`
**Touches:** Decorations on FuncDecl params/results; updates
`ctx.MultilineSigs`.
**What:** When a func signature's single-line form exceeds the limit,
wraps at parameter boundaries. First param stays on the open-paren line,
subsequent params each on their own line, closing `)` stays attached to
last param (doc forbids dangling close-parens on sigs). Single-param sigs
and return-list-only wrapping are NOT yet handled.
**Order:** Early. R2 needs `MultilineSigs` to be final.

## R4 — function-call wrapping (pack-or-spread)
**File:** `pass_funccall.go`
**Touches:** Arg decorations on `*dst.CallExpr`; writes to
`ctx.OuterHandled`.
**What:** For each CallExpr, picks one of three layouts via
`decideCallLayout`:
1. **layoutCollapse** — projected single-line width ≤ limit; clear arg
   newlines, inline.
2. **layoutSymmetric** (R6 inside R4) — some arg is a multi-line container
   (composite literal, FuncLit closure, `&Composite{}`, nested call); the
   "outer args inline up to the container's open token, container close +
   trailing args + `)` share a closing line." Container may be at any
   position, not just last (`dstutil.Apply(file, func(){...}, nil)`
   works). See `openTokenWidth` + `isMultiLineContainer`.
3. **layoutPack** — every arg on its own continuation line, `)` on its
   own line at the call indent.

**Three opt-outs:**
- R5 (formatting func allowlist) — handled inline via
  `applyFormattingCallLayout` if `isFormattingCall`.
- R8 (structured-log) — `isStructuredLogCall` short-circuits.
- **Layout-fragile parent skip** — when the call is multi-line in source
  AND every source line fits AND it sits inside a `KeyValueExpr` of a
  CompositeLit OR a `BinaryExpr` with any NewLine in its subtree (R16
  operator-broken), R4 leaves it alone. Stops the oscillation we kept
  hitting; see `gotchas.md#fragile-parents`.

**Marks calls in `OuterHandled`** when committing to layoutSymmetric
(via `markInnerCallsHandledDeep`) or layoutPack (via
`markInnerCallsHandled`).

**Order:** Mid-late. Must run AFTER R7/R8/R16 (so they can mark calls
OuterHandled or reshape composites first) and BEFORE R9 (so R9 sees the
final wrap and can budget chunks against the post-wrap indent).

## R5 — compact wrap for formatting functions
**File:** `pass_funccall.go::applyFormattingCallLayout`
**Activated by:** R4 when `isFormattingCall(call, fmtFns, denyFns)` is
true.
**What:** For fmt.Errorf / log.*f / t.Errorf / require.Errorf style calls,
splits the format string with `+` so line 1 (`f(... "part1" +`) fits, then
puts `"part2"` on a continuation line with the remaining args inline after
it. Preserves valid multi-line layouts whose every source line already
fits (`allCallLinesFit`).
**Config:** `Config.FormattingFuncs` (default list), `FormattingFuncsDeny`.

## R6 — indentation symmetry
**File:** `pass_funccall.go::decideCallLayout` (the `layoutSymmetric`
branch).
**Activated by:** R4 when an arg satisfies `isMultiLineContainer` AND
projected pre-line + post-line fit.
**Container types:** `CompositeLit`, `FuncLit`, `CallExpr`, `UnaryExpr`
wrapping any of the above (`&Foo{}`).
**Important quirk:** When the container is a FuncLit, the symmetric form
shifts the closure body's effective indent to `(callIndent + tab)`, which
may be SHALLOWER than the source. R4 measures inner-call wrap decisions
from source positions, so without intervention it would over-wrap inner
calls. `markInnerCallsHandledDeep` (NOT `markInnerCallsHandled`) is used
here — it descends into FuncLit bodies to mark all inner CallExprs as
handled.

## R7 — composite-literal reflow
**File:** `pass_complit.go`
**What:** Reflows a composite literal whose source span has ANY line over
the limit:
- **Keyed** composites (struct & map, `KeyValueExpr` elts) → one element
  per line (doc forbids packing struct fields).
- **Non-keyed** (slice/array) → pack elements greedily up to the limit.
**Detection:** `anyCompositeLineOverLimit` walks every source line from
Lbrace.Line to Rbrace.Line. Was originally only single-line composites;
extended to multi-line because packed-but-overlong inner lines need
reflow too.
**Order:** Before R4 so R4 sees the reflowed composite as a multi-line
container and can apply layoutSymmetric.

## R8 — structured-log call rules
**File:** `pass_strlog.go`
**What:** Methods named (default) `TraceS` / `DebugS` / `InfoS` / `WarnS`
/ `ErrorS` / `CriticalS` get a tailored layout: `ctx` and `msg` on the
call line, each attr on its own line, closing `)` attached to the last
attr (unlike R4's pack form). Also LINTS: if `msg` isn't a static string
literal, emits a warning diag.
**Config:** `Config.StructuredLogMethods`.
**Order:** Before R4 so R8 owns these calls and marks inner attrs as
OuterHandled.

## R9 — string-literal reflow
**File:** `pass_strlit.go`
**What:** Walks every outermost string expression (lone BasicLit or
`"a" + "b" + ...` chain), joins, re-splits at line-fitting boundaries.
Three behaviours route through one code path:
- Long BasicLit on overlong source line → split until each chunk fits.
- Concat with suboptimal split positions → rejoin + re-split.
- Concat whose joined body fits a single continuation line → replace with
  a single literal (subsumes the old "string-join" rule).

**Key helpers:**
- `countWrappedAncestors` — counts wrap-ancestors that shift the string's
  effective render column. CallExpr (if `isCallWrapped`) and BinaryExpr
  (if `subtreeHasNewLineDec` — R16's operator split). Each contributes one
  tab.
- `inMultilineContainer` — multi-line CompositeLit / wrapped CallExpr
  ancestor. Used by the "move whole literal onto its own line" path
  (avoids `"Crit"+"icalS"` mid-word splits in slice literals).
- `insideLayoutFragileParent` — same as R4. Skips reflow for concats
  inside a KeyValueExpr of a CompositeLit when every source line fits.

**Skip conditions:**
- Backtick raw strings.
- Literals containing backslashes WITHOUT spaces (would mid-escape split).
- Calls in the formatting-funcs allowlist (R5 owns those).

**Order:** After R4 (so wrap state is final).

## R10 — line-length check (warn-only)
**File:** `pass_linelen.go`
**Hook:** Not in the pass pipeline. Called directly in `Format()` AFTER
`decorator.Fprint`.
**What:** Walks the rendered output line-by-line and emits a diagnostic
for each over-limit line. Honors `//nolint:ll` on the offending line.

## R11 — stanza spacing
**File:** `pass_stanza.go`
**Touches:** `Before` decoration on block statements.
**What:** Inside a block, any statement (other than the first) carrying a
leading comment gets a blank line before it. Conservative — only adds the
break where the developer already placed a comment.
**Order:** Late. Sees the final block layout.

## R12 — body split (single-line func body → multi-line when over)
**File:** `pass_bodysplit.go`
**What:** When `func F() T { stmt }` or `func() T { ... }` is single-line
in source AND that line exceeds the limit, sets
`body.List[0].Decorations().Before = dst.NewLine`. gofmt then promotes the
whole body to multi-line.
**Critical follow-up:** Marks every inner `CallExpr` in the now-split
body as `OuterHandled`. R4 measures call positions from SOURCE columns,
which after a body split are stale (the body lived on the over-limit
single line). Without this, R4 over-wraps every call inside the body.
**Order:** Very early. Other passes (R7, R8, R4) want to see the body as
multi-line.

## R13 — var-block wrap
**File:** `pass_varblock.go`
**What:** An ungrouped `var a, b, c T` declaration whose line exceeds the
limit becomes `var ( ... )` block with names packed across spec lines.
gofmt auto-aligns trailing types.
**Scope:** `var` only (no const/type). Single ValueSpec. No values
(`var x = 1` excluded).
**Order:** After R4 (var statements may contain wrappable calls).

## R15 — comment reflow
**File:** `pass_comment.go`
**What:** Single-line `//`-style comments that exceed the limit get
reflowed onto multiple lines at word boundaries (rightmost space within
budget; falls back to budget position for no-space text).
**Critical constraint:** Only reflows when at least one paragraph line is
over the budget. Re-flowing fitting prose was the source of stability
churn — the limit is a ceiling, not a target.
**Paragraph breakers** (flushed verbatim, not joined): tool directives
(`//go:`, `//nolint:`, `//line`), block comments, empty `//` lines,
indented continuations (≥ 2 leading spaces), list markers (`- `, `* `,
`+`, `N. `, `N) `).
**Order:** Last. Independent of any prior pass.

## R16 — binary-op chain break
**File:** `pass_binaryop.go`
**Touches:** `Before` decoration on the RHS operands of `BinaryExpr`s in
a same-operator left spine.
**What:** When a statement's source line exceeds the limit AND its outer
expression is a binary chain of `&&` / `||` / `+` / `-` / `*` / `/` /
`&` / `|` / `^` / `<<` / `>>` / `&^`, prefer breaking AFTER the operator
over wrapping operand calls.
**Algorithm:**
1. Collect the left spine of same-operator BinaryExprs:
   `A op B op C op D` parses as `(((A op B) op C) op D)`;
   `dstChain[0]` is outermost, `dstChain[N-1]` innermost.
2. Compute operand widths in source order via `astExprJoinedWidth`.
3. Try `k = 0, 1, 2, ..., N` (k = number of operators broken, from outer
   in). For each `k`, `binaryLinesFit` checks every resulting line.
4. Apply the smallest `k` that fits; clear stale operator breaks if no
   `k` fits (so R4 owns the wrap unambiguously).
5. When `k > 0`, mark every inner CallExpr in the chain as
   `OuterHandled` and clear their inner arg wraps — operand source
   positions are no longer authoritative.

**Width measurement detail (load-bearing):** `prefixW` and `suffixW` are
computed via `sourceWidth(stmt.Pos(), bin.Pos())` / `sourceWidth(bin.End(),
hdrEnd)`. This naturally captures `"if "`, `"return "`, `"x := "`,
`"if init; "`, ` " {"` uniformly. **Earlier-bug warning:** an earlier
implementation hard-coded `stmtPrefixWidth(IfStmt) = 3` etc., which broke
on AssignStmt (`contIndent := f(...) + tab`) — the LHS prefix was
missing. The source-span approach is the right answer; don't regress to
hardcoded keyword lengths.

**Rendered-indent calc (load-bearing):** `stmtRenderedIndent` walks
parents to the enclosing BlockStmt, then steps ONE MORE PARENT UP to the
block's owner (FuncDecl / IfStmt / ForStmt), and uses THAT node's first-
line indent + tab. Using the block's Lbrace line was wrong for multi-line
function signatures — Lbrace sits on a continuation line at owner-
indent + tab, so the calc was overcounting by one tab.

**Skip:** When the enclosing block is single-line in source (R12
territory), R16 bails — source widths describe the body-collapsed form,
not the post-R12 layout.

**Order:** Before R4. Marks operand calls as OuterHandled so R4 leaves
them alone after R16 commits to operator-break form.

## Pipeline order (canonical)

From `passes.go`:

```
1. R1   switchCaseSpacing
2. R3   funcDefWrap
3. R12  bodySplit
4. R7   compositeLitReflow
5. R8   structuredLogWrap
6. R16  binaryOpWrap
7. R4   funcCallWrap            (R5 + R6 inside)
8. R9   stringLitWrap
9. R13  varBlockWrap
10. R2  funcSignatureBodyBlank
11. R11 stanzaSpacing
12. R15 commentReflow
       (then: decorator.Fprint, then R10 line-length check)
```

Reasoning for the trickiest slots (paraphrased from inline comments):

- **R7 before R4** — R4 needs to see reflowed composites as multi-line
  containers (so layoutSymmetric fires).
- **R8 before R4** — R8 owns its calls; pre-marking OuterHandled keeps R4
  off them.
- **R16 before R4** — R16's operator break decides whether operand calls
  should be wrapped; if R16 commits, it marks them OuterHandled.
- **R9 after R4** — R9 budgets string chunks against the post-wrap
  indent; the call's wrap state must be final.
- **R2 after R3 / R4 / R16** — header multi-line-ness depends on what
  these did.

When in doubt: the order is justified by the comments in `passes.go`.
Read them before reshuffling.
