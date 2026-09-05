package r14

import "fmt"

const (
	// Limit is a long documentation comment that fits outside a block but
	// requires wrapping once the declaration is collected.
	Limit = 10
)

// Counts describes the original block.
const (
	// Small is the smaller value.
	Small = 1
	Large = 2
) // end of counts

var (
	// First documents a variable before a function.
	First = next()

	// Second follows First during initialization.
	Second = next()
	Third  = next() // trailing comment
	Last   = next()
)

func next() int { return 1 }

type Implementation struct{}

// Verify the implementation.
var _ fmt.Stringer = (*Implementation)(nil)

func (*Implementation) String() string { return "" }

func local() {
	const x = 1
	var y = x
	fmt.Println(y)
}
