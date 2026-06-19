package format

// RuleDoc is a one-screen explanation of a single rule, exposed to the CLI via
// the --explain flag and to in-IDE diagnostics.
type RuleDoc struct {
	ID      string
	Title   string
	Summary string
}

// Rules returns the docs for every rule (and meta-rule) goformat knows about.
// The list reflects what's actually implemented in v0.1; deferred rules (R6,
// R7) appear with status notes so users aren't surprised when the formatter
// doesn't apply them.
func Rules() []RuleDoc {
	return []RuleDoc{
		{
			ID:    "R1",
			Title: "Switch / select case spacing",
			Summary: `A switch or select with two or more case clauses gets a blank
line between consecutive clauses. Leading comments on a case stay attached
to the case; the blank line goes above them.`,
		},
		{
			ID:    "R2",
			Title: "Body blank line after multi-line header",
			Summary: `When a function signature, function literal header, or
control-flow header (if / for / range / switch / type-switch) spans more
than one line — either because it was written that way or because R4
wrapped a call inside the header — the body starts with a blank line so
the multi-line header doesn't bleed visually into the first body
statement. SelectStmt is excluded — "select" never multi-lines.`,
		},
		{
			ID:    "R3",
			Title: "Function-definition wrapping",
			Summary: `A function signature whose single-line form exceeds the
line limit is wrapped at parameter boundaries. The first parameter stays
on the same line as the opening "(", subsequent parameters move to their
own line, and the closing ")" stays attached to the last parameter (the
doc forbids dangling close-parens on function definitions). Single-
parameter signatures and return-list-only wrapping are not yet handled.`,
		},
		{
			ID:    "R4",
			Title: "Function-call wrapping (pack-or-spread)",
			Summary: `A function call whose single-line form exceeds the line
limit is wrapped via a greedy packer: outer args + container open token
fill each continuation line up to the limit, with the closing ")" on its
own line. When a multi-line input could collapse to single-line (or to
an inline-symmetric / wrapped-symmetric form), R4 reflows in that
direction. Method-chain calls (callee spans multiple lines) are left
alone — source-positional measurements give wrong answers for them.`,
		},
		{
			ID:    "R5",
			Title: "Compact wrap for formatting functions",
			Summary: `Calls to formatting-style functions (fmt.Errorf, log.Debugf,
fmt.Printf, t.Errorf, t.Fatalf, …) get the doc's compact wrap when
they're overlong: the format-string is split with "+" and remaining
args follow inline. The allow/deny lists are user-configurable via
formatting_funcs / formatting_funcs_deny in goformat.toml. R5 also
PRESERVES multi-line layouts whose every source line already fits —
the developer's choice of split point is respected.`,
		},
		{
			ID: "R6",
			Title: "Indentation symmetry (preserve and produce " +
				"nested-call symmetric form)",
			Summary: `When a call's last argument is a multi-line "container"
(call, composite literal, function literal, &Composite) and the
synthesised first line — outer args inline up to and including the
container's open token — fits within the limit, R4 emits the doc's
preferred nested form rather than the verbose one-arg-per-line layout:

    sort.Slice(items, func(i, j int) bool {
        return items[i] < items[j]
    })

    addrs = append(addrs, &lnwire.NetAddress{
        IdentityKey: update.IdentityKey,
    })

Already-symmetric inputs are preserved (the rule is a no-op on them).
Verbose inputs that COULD be symmetric are actively rewritten. When the
first line wouldn't fit, R4 falls back to the verbose pack — each arg
on its own continuation line with ")" on its own line.`,
		},
		{
			ID: "R7",
			Title: "Composite-literal reflow (struct " +
				"one-per-line, slice packed)",
			Summary: `A single-line composite literal whose source line
exceeds the limit is reflowed onto multiple lines:

  * Keyed composites — struct & map literals (KeyValueExpr elts) — get
    ONE element per line. Packing struct fields across lines is
    forbidden by the doc (ruins git-diff hygiene).
  * Non-keyed composites — array & slice literals — pack elements
    greedy-style like function args, filling each line up to the limit.

R7 runs before R4 so the call-wrap pass sees R7-reflowed composites as
multi-line containers and can emit the inline-symmetric form
("append(slice, &T{ ... })"). Multi-line composites in source are left
alone — the developer's chosen layout is preserved.`,
		},
		{
			ID:    "R8",
			Title: "Structured-log call rules",
			Summary: `Methods like log.InfoS(ctx, "msg", attrs...) get a tailored
layout: attrs each on their own line, closing ")" attached to the last
attr (not on its own line, unlike R4). R8 also LINTS: if the msg argument
isn't a static string literal, a warning is emitted. v0.1 detects log
calls by exact method-name match (TraceS / DebugS / InfoS / WarnS / ErrorS
/ CriticalS by default; configurable via structured_log_methods).`,
		},
		{
			ID: "R9",
			Title: "String-literal reflow (split, join, and " +
				"re-split)",
			Summary: `R9 is a full string-reflow rule. It walks every
outermost string expression — a lone interpreted string literal OR a
"+" concat chain of string literals — gathers the chunks into one
logical body, then re-splits the body into N chunks sized to fill each
line up to the limit.

This means three behaviours route through the same code path:
  * Long literal needing a split: keep splitting until each chunk fits.
  * Existing concat chain with suboptimal split positions: rejoin and
    re-split (the user's "reflow").
  * Concat whose joined form fits on a single continuation line: replace
    with a single literal (R9 subsumes the old "string-join" rule).

Effective indent is computed from the dst parent chain — every wrapped
enclosing CallExpr pushes the string one tab deeper, mirroring R4's
actual wrap layout. Subsequent chunks land one tab past the first.

Skipped: backslash-escape literals (mid-escape split would corrupt the
value), backtick raw strings, and calls in the formatting-funcs allowlist
(R5 owns those layouts).`,
		},
		{
			ID:    "R10",
			Title: "Line-length check (warn-only)",
			Summary: `Any line whose visual width (tabs counted as tab_width
columns) exceeds line_length surfaces a warning, unless it carries
//nolint:ll. R10 doesn't modify code; it catches what every other rule
left behind.`,
		},
		{
			ID: "R11",
			Title: "Stanza spacing (blank line before comment-led " +
				"stmt)",
			Summary: `Inside a block, any statement (other than the first) that
carries a leading comment gets a blank line before it. This matches the
doc's "logical stanzas" rule, restricted to the conservative signal we
can detect — a comment marking a new section. We never invent stanza
breaks where the developer didn't already place a comment.`,
		},
		{
			ID: "R12",
			Title: "Body split (single-line func body whose line " +
				"exceeds limit)",
			Summary: `When a function definition or function literal fits on
one line (` + "`func F(args) T { body }`" + `) and that line is over the
limit, force the body's first statement onto its own line. gofmt then
multi-lines the whole body — the closing ` + "`}`" + ` follows on its
own line at the function's indent.`,
		},
		{
			ID: "R13",
			Title: "Var-block wrap (long single-line var decl → " +
				"var block)",
			Summary: `An ungrouped ` + "`var a, b, c, ... T`" + ` declaration
whose line exceeds the limit is rewritten as a grouped ` + "`var ( ... )`" + `
block, with the names greedy-packed across spec lines. gofmt auto-aligns
the trailing types. Scope: var only (no const/type), single ValueSpec,
no values (no ` + "`var x = 1`" + ` form).`,
		},
		{
			ID: "R16",
			Title: "Binary-op break (split at &&/||/+ before " +
				"wrapping calls)",
			Summary: `When a statement's source line exceeds the limit
and its outer expression is a binary chain (&&, ||, +, -, *, /, &, |, ^, <<,
>>, &^), prefer to break AFTER the operator instead of wrapping the operand
sub-expressions. The split puts the operator at the end of the line and the
right operand on a continuation line one tab deeper —

    if strings.ContainsRune(ch, '\') &&
        !strings.ContainsRune(ch, ' ') {

R16 runs before R4 and marks every operand call as OuterHandled, so R4
leaves those calls alone. Comparison ops (==, !=, <, …) are NOT split —
they're internal to a wider logical chain and breaking at a comparison
would orphan one of its operands.`,
		},
		{
			ID: "R15",
			Title: "Comment reflow (split long `//`-style " +
				"comments)",
			Summary: `A single-line "//"-style comment that exceeds the limit
is split into multiple comment lines at the rightmost word boundary
within budget. Tool directives (//go:build, //nolint:…, //line) and
block comments (/* … */) are left alone; comments with no internal
spaces (URLs, decorative dividers) are also left as-is — splitting at
a non-space boundary would obscure them.`,
		},
	}
}

// ExplainRule looks up a rule by its ID (e.g. "R3"). Returns nil if not found.
func ExplainRule(id string) *RuleDoc {
	for _, r := range Rules() {
		if r.ID == id {
			return &r
		}
	}
	return nil
}
