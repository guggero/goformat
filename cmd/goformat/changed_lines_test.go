package main

import (
	"bytes"
	"testing"

	"github.com/guggero/goformat/internal/config"
)

func TestChangedLines(t *testing.T) {
	src := []byte(
		`package p

func untouched( ){ println( 1 ) }

func changed() {
 println( 2,3 )
}
`,
	)
	want := []byte(
		`package p

func untouched( ){ println( 1 ) }

func changed() {
	println(2, 3)
}
`,
	)
	got, _, err := formatChangedLines(
		src, "test.go", config.Default(), []lineSpan{
			{
				5, 6,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("changes escaped the selected line:\n%s", got)
	}
}

func TestChangedLinesRejectPartialWrap(t *testing.T) {
	src := []byte(
		`package p

func f() {
 run(first,
 second)
}

func g() {
 println( 2,3 )
}
`,
	)
	want := []byte(
		`package p

func f() {
 run(first,
 second)
}

func g() {
	println(2, 3)
}
`,
	)
	got, skipped, err := formatChangedLines(
		src, "test.go", config.Default(), []lineSpan{
			{
				3, 4,
			}, {
				8, 9,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !skipped || !bytes.Equal(got, want) {
		t.Fatalf(`partial wrap was applied or independent hunk lost (skipped=%v):
%s`, skipped, got)
	}
}

func TestGitChangesExpandStatement(t *testing.T) {
	baseline := []byte(
		`package p

func f() {
	run(first,
		second)

	println( 7,8 )
}

func unrelated( ){println( 9 )}
`,
	)
	src := bytes.Replace(
		baseline, []byte("run(first,"), []byte("run(first, third,"), 1,
	)
	got, skipped, err := formatGitChanges(
		baseline, src, "test.go", config.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Replace(
		baseline, []byte("run(first,\n\t\tsecond)"),
		[]byte("run(first, third, second)"), 1,
	)
	if skipped || !bytes.Equal(got, want) {
		t.Fatalf(`complete statement not formatted independently (skipped=%v):
%s`, skipped, got)
	}
}

func TestGitChangesCollectVariable(t *testing.T) {
	baseline := []byte(
		`package p

const (
	Limit = 2
)

var (
	First = next()
)

func next( )int{return 1}

func unrelated( ){println( 9 )}
`,
	)
	src := append(bytes.Clone(baseline), []byte(
		"\n// Second follows First.\nvar Second = next()\n",
	)...)
	got, skipped, err := formatGitChanges(
		baseline, src, "test.go", config.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Replace(
		baseline, []byte("First = next()\n)"), []byte(
			`First = next()

	// Second follows First.
	Second = next()
)`,
		), 1,
	)
	if skipped || !bytes.Equal(got, want) {
		t.Fatalf(`variable not collected independently (skipped=%v):
%s
want:
%s`, skipped, got, want)
	}
}

func TestGitChangesInsideClosure(t *testing.T) {
	baseline := []byte(
		`package p

func f() {
	run(func() {
		println(1)
		println( 2 )
	})
}
`,
	)
	src := bytes.Replace(
		baseline, []byte("println(1)"), []byte("println( 3,4 )"), 1,
	)
	got, skipped, err := formatGitChanges(
		baseline, src, "test.go", config.Default(),
	)
	want := bytes.Replace(
		baseline, []byte("println(1)"), []byte("println(3, 4)"), 1,
	)
	if err != nil || skipped || !bytes.Equal(got, want) {
		t.Fatalf("closure edit escaped its statement: %v, "+
			"skipped=%v\n%s", err, skipped, got)
	}
}

func TestGitChangesDeletion(t *testing.T) {
	baseline := []byte(
		`package p

func f() {
	run(
		first,
		second,
		third,
	)
}

func untouched( ){println( 9 )}
`,
	)
	src := bytes.Replace(baseline, []byte("\t\tsecond,\n"), nil, 1)
	cfg := config.Default()
	cfg.Optimize = true
	got, skipped, err := formatGitChanges(baseline, src, "test.go", cfg)
	want := bytes.Replace(
		src, []byte("run(\n\t\tfirst,\n\t\tthird,\n\t)"),
		[]byte("run(first, third)"), 1,
	)
	if err != nil || skipped || !bytes.Equal(got, want) {
		t.Fatalf("deleted argument not handled locally: %v, "+
			"skipped=%v\n%s", err, skipped, got)
	}
	baseline = []byte(
		`package p

func removed() {}

func untouched( ){println( 9 )}
`,
	)
	src = bytes.Replace(baseline, []byte("func removed() {}\n\n"), nil, 1)
	got, _, err = formatGitChanges(baseline, src, "test.go", cfg)
	if err != nil || !bytes.Equal(got, src) {
		t.Fatalf("deleted function selected its neighbor: %v\n%s", err, got)
	}
}

func TestGitChangesNoformatDoesNotCollect(t *testing.T) {
	baseline := []byte(
		"package p\n\nvar Unrelated=1\n\nfunc f( ){println( 1 )}\n",
	)
	src := append(bytes.Clone(baseline), []byte(
		"\n//noformat\nvar   Protected=2\n",
	)...)
	got, skipped, err := formatGitChanges(
		baseline, src, "test.go", config.Default(),
	)
	if err != nil || skipped || !bytes.Equal(src, got) {
		t.Fatalf("protected addition triggered formatting: %v, "+
			"skipped=%v\n%s", err, skipped, got)
	}
}

func TestGitChangesLongLocalVar(t *testing.T) {
	baseline := []byte(
		`package p

func f() {
	var firstLongCounter, secondLongCounter [4]int
}

func untouched( ){println( 9 )}
`,
	)
	src := bytes.Replace(
		baseline, []byte("secondLongCounter"),
		[]byte(
			"secondLongCounter, thirdLongCounter, "+
				"fourthLongCounter, fifthLongCounter",
		),
		1,
	)
	got, skipped, err := formatGitChanges(
		baseline, src, "test.go", config.Default(),
	)
	if err != nil || skipped || !bytes.Contains(got, []byte(
		"var (",
	)) || !bytes.HasSuffix(got, []byte(
		"func untouched( ){println( 9 )}\n",
	)) {

		t.Fatalf("local var wrap was not isolated: %v, skipped=%v\n%s", err, skipped, got)
	}
}
