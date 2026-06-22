# Testing

## Layout

```
testdata/
  R<N>_<short>/
    <case>.in.go      Input source
    <case>.out.go     Expected output after Format()
  baseline_noop/      Files that should round-trip unchanged
```

`R<N>_<short>` directories group cases by the primary pass under test
(R1_switch, R2_funcsig, R3_funcdef, …). A case is identified by its
`<case>` basename; both `.in.go` and `.out.go` must exist.

`baseline_noop/` is the negative-control corpus: inputs that are already
well-formatted should produce identical output. Add to this whenever you
notice a real-world file that surfaces a stability issue.

## Running

```bash
# Everything
go test ./...

# Just the table-driven pair tests
go test ./internal/format/ -run TestUnit

# A single case
go test ./internal/format/ -run 'TestUnitPairs/R16_binaryop/if_and_long' -v

# With the failing diff printed
go test ./internal/format/ -run 'TestUnitPairs/R9_strlit/multiline' -v
# (format_test.go uses a unified diff in the failure message)
```

## Adding a case

1. Pick the rule it primarily exercises and the matching `R<N>_<name>`
   directory (create one if it's new).
2. Write `<case>.in.go` — minimal Go source that triggers the behaviour.
3. Either:
   - Run the formatter on it manually:
     `/tmp/goformat-bin <case>.in.go > <case>.out.go`
     and inspect the output before committing, OR
   - Hand-write `<case>.out.go` to capture the intended layout
     (preferred when fixing a regression — encode the desired behaviour
     before writing the fix).
4. `go test ./internal/format/ -run TestUnit` to confirm.

Cases should be SMALL — a function or two, just enough to exercise the
layout decision. Resist the temptation to paste a 200-line real-world
function; it makes future test changes hairy and obscures what's being
tested.

## Test types

- `format_test.go::TestUnitPairs` — table-driven, walks `testdata/`,
  reads each `.in.go`, formats it, asserts equality with `.out.go`.
- `pass_linelen_test.go` — direct tests for R10's diagnostic emission.
- `pass_strlog_test.go` — direct tests for R8's lint warnings.
- `internal/config/*_test.go` — config loading.

There's no integration test against a real codebase in CI — that's
ad-hoc, done by formatting `../../go/src/github.com/lightningnetwork/lnd`
or `.../btcsuite/btcd` and reviewing diffs.

## Idempotency check

Not a test in CI; do this manually after substantial layout changes:

```bash
cp -r internal/format /tmp/_a
/tmp/goformat-bin -w internal/format/*.go
diff -r /tmp/_a internal/format    # diff = work done by formatter

cp -r internal/format /tmp/_b
/tmp/goformat-bin -w internal/format/*.go
diff -r /tmp/_b internal/format    # MUST be empty (idempotent)
```

If the second diff is non-empty, the formatter oscillates. Read
[`gotchas.md#oscillation`](gotchas.md#oscillation).

Some files stabilise in 2 runs rather than 1 — currently R15's comment
reflow when R6 shifts indents. Accept ≤ 2 runs to stability; ≥ 3 is a
bug.

## Real-world test benches

```bash
# btcd psbt (smaller; faster to scan diffs)
cd ../../go/src/github.com/btcsuite/btcd
git checkout psbt/    # reset
~/path/to/goformat -d psbt/*.go | less

# lnd (huge; can take a minute)
cd ../../go/src/github.com/lightningnetwork/lnd
git checkout .        # reset (CAREFUL with WIP)
~/path/to/goformat -d ./...
```

When pulling test data into the repo's `testdata/`, minimise it first.
Real lnd / btcd code has lots of unrelated noise.

## Debugging a failing test

The failure message shows `--- got ---` and `--- want ---` blocks; the
mismatched bytes are usually obvious in context. Common causes:

- **Whitespace** — gofmt inserts tab/space carefully; `cat -A` on both
  to see the actual bytes.
- **Alignment** — gofmt aligns keyed struct fields. A test that expects
  specific spacing may need re-running once if you've added a new field
  group.
- **Decoration mismatch** — `Before = NewLine` vs `EmptyLine` produces a
  one-line diff (single vs blank). Easy to miss visually.

To debug interactively: add a `fmt.Fprintf(os.Stderr, ...)` in the pass,
re-run `go test`. Output goes to stderr; `go test` captures stdout/stderr
on failure but not on success — temporarily make the test fail
(`t.Fatal("debug")`) to surface it.

## CI

Configured in `.claude/`-adjacent CI hooks (look in `.github/workflows/`
if present). Locally, `go test ./... && go vet ./...` is the gate.
