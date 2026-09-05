package r14

import "fmt"

// Constants describes this existing block, not its first element.
const (
	// One documents an element.
	One = 1
)

const (
	// Two documents a standalone constant.
	Two = 2
)

// More constants have a different header.
const (
	Three = 3
)

var (
	// First documents a standalone variable.
	First = next()
)

// Grouped variables have their own documentation.
var (
	// Second documents an element of the existing block.
	Second = next()
)

var (
	// Standalone documents another standalone variable.
	Standalone = next()
)

// Another variable block has a different purpose.
var (
	Third = next()
)

var (
	// Default is collected while the assertion and block header stay here.
	Default = 1
)

func next() int { return 1 }

type T struct{}

func (*T) String() string { return "" }

// Check the implementation and define a default.
var (
	_ fmt.Stringer = (*T)(nil)
)
