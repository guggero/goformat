package r14

import "fmt"

// First documents a variable before a function.
var First = next()

func next() int { return 1 }

// Limit is a long documentation comment that fits outside a block but requires wrapping once the declaration is collected.
const Limit = 10

type Implementation struct{}

// Verify the implementation.
var _ fmt.Stringer = (*Implementation)(nil)

func (*Implementation) String() string { return "" }

var (
	// Second follows First during initialization.
	Second = next()
	Third = next() // trailing comment
)

// Counts describes the original block.
const (
	// Small is the smaller value.
	Small = 1
	Large = 2
) // end of counts

func local() {
	const x = 1
	var y = x
	fmt.Println(y)
}

var Last = next()
