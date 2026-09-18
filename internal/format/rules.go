package format

// RuleDoc is a one-screen explanation of a single rule, exposed to the CLI via
// the --explain flag and to in-IDE diagnostics.
type RuleDoc struct {
	ID      string
	Title   string
	Summary string
}

// Rules returns the docs for every rule (and meta-rule) goformat knows about.
// R1–R16 can be selected with --rule. The noformat entry documents an always-on
// protection directive rather than a selectable transformation.
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
			Summary: `Function signatures pack as many parameters or return
types onto each line as fit within the limit. Closing parentheses stay
attached to the last field; the first return type stays with its opening
parenthesis. The first parameter may move to a continuation line when
needed. Multi-line signatures end with the body's opening "{" on an
indented line, followed by a blank line before the body.

By default, fitting signatures retain their layout. --optimize also
repacks fitting signatures and collapses them when possible. Signatures
with comments, multi-line field types, or no fitting layout at legal
break points are preserved.

Closure signatures use the same packer after call arguments are laid
out. Reflowed closures are repacked even without --optimize. A collapsed
header loses its separating body blank; deliberate blanks after an
already-single-line header remain.`,
		},
		{
			ID:    "R4",
			Title: "Function-call wrapping (pack-or-spread)",
			Summary: `A function call whose single-line form exceeds the line
limit is wrapped via a greedy packer: arguments share continuation lines
with a multi-line argument's opening and closing tokens, with the outer
closing ")" on its own line. For example:

    require.Eventually(
        t, func() bool {
            return ready()
        }, timeout, interval,
        "waiting for readiness",
    )

When a multi-line input could collapse to single-line (or to
an inline-symmetric / wrapped-symmetric form), R4 reflows in that
direction. Partially wrapped calls are corrected even when all lines
fit, without --optimize. Already-valid layouts are compacted only with
--optimize. Formatting and structured-log calls keep their exceptions.
Method-chain calls (callee spans multiple lines) are left
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

Already-symmetric inputs are preserved, including multiple nested calls
sharing a composite literal's opening line and closing together.
Successive multi-line containers may also share a line that closes one
and opens the next, provided each opens and closes at the same indentation:

    append([]byte{
        0x00, 0x14,
    }, bytes.Repeat([]byte{
        0x01,
    }, 20)...)

Anonymous struct literals follow the same rule: in struct { ... }{ ... },
the type's opening brace starts the first container. An argument before
it can share that line, both in inline and wrapped calls.

With --optimize, a valid fitting call is rewritten to symmetry only if
the result uses fewer total lines. Equal or longer layouts stay as written,
including a wrapped outer call with a single-line nested argument. Invalid
partial wrapping and overlong calls are still corrected. When the
first or closing line wouldn't fit, R4 packs continuation lines around
the container's opening and closing tokens, with the outer ")" on its
own line.`,
		},
		{
			ID: "R7",
			Title: "Composite-literal reflow (struct " +
				"one-per-line, slice packed)",
			Summary: `A single-line composite literal whose source line
exceeds the limit is reflowed onto multiple lines:

  * Keyed composites — struct & map literals (KeyValueExpr elts) — get
    ONE element per line. This also applies to literals already spanning
    multiple lines, even when every existing line fits. Multiple fields
    may share a line only when the whole initializer fits on one line.
  * Non-keyed composites — array & slice literals — pack elements
    greedy-style like function args, filling each line up to the limit.

R7 runs before R4 so the call-wrap pass sees R7-reflowed composites as
multi-line containers and can emit the inline-symmetric form
("append(slice, &T{ ... })"). Existing blank lines and field comments are
preserved. Multi-line non-keyed composites retain their chosen packing.`,
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

When R4 reflows an argument, R9 reflows its string at the new indentation
in the same pass, even without --optimize. A split string is joined when
it now fits on one line. Otherwise fitting strings retain their layout
by default.

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
the trailing types. This also splits long name lists inside existing or
collected var blocks. Scope: var only (no const/type), no values (no ` + ("`va" +
				"r x = 1`") + ` form).`,
		},
		{
			ID:    "R14",
			Title: "Collect package constants and variables",
			Summary: `Package declarations collect after imports: const blocks first,
then var blocks, including single declarations. Standalone declarations bring
their Godoc inside the collected block, reflowing at the new indentation.
Existing documented blocks stay separate, with their headers above them.
Local declarations keep their scope.

Typed blank-variable assertions remain in place. Const declarations immediately
following a type declaration remain beside that type when every value has that
type, regardless of its underlying representation (including strings and
booleans). Type aliases and conversions are recognized within the file;
the underlying type need not be resolved.
Independent iota groups keep separate blocks so their values cannot change.

Variable initialization order is preserved. Protected declarations and
potentially effectful assertions form ordering boundaries. Mixed blocks with
such assertions stay together. Enabled without --optimize; configurable with
[rules] declaration_grouping.`,
		},
		{
			ID:    "noformat",
			Title: "Preserve source exactly",
			Summary: `A standalone //noformat or // noformat protects the immediately
following physical line. If that line opens a multi-line construct, protection
extends through its closing delimiters, including nested calls and closures.
Place the directive directly above the code, after any Godoc comment.

Protected bytes are restored verbatim after printing, including whitespace.
All formatting rules, declaration movement, and line-length diagnostics are
suppressed there. This applies in both default and --optimize modes.`,
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
