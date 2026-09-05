package syntax

import (
	"bytes"
	"testing"
)

func TestVariableGrouping(t *testing.T) {
	before, err := Fingerprint([]byte(
		"package p; func f() { var a, b [4]int }",
	))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source string
		equal  bool
	}{
		{"package p; func f() { var (a [4]int; b [4]int) }", true},
		{"package p; func f() { var (a [4]int; b []int) }", false},
		{"package p; func f() { var (b [4]int; a [4]int) }", false},
	} {

		after, err := Fingerprint([]byte(tc.source))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(before, after) != tc.equal {
			t.Fatalf("incorrect equivalence for %s", tc.source)
		}
	}
}
