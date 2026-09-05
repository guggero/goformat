package r14

import "fmt"

// First documents a standalone variable.
var First = next()

func next() int { return 1 }

// Grouped variables have their own documentation.
var (
	// Second documents an element of the existing block.
	Second = next()
)

// Standalone documents another standalone variable.
var Standalone = next()

// Another variable block has a different purpose.
var (
	Third = next()
)

// Constants describes this existing block, not its first element.
const (
	// One documents an element.
	One = 1
)

// Two documents a standalone constant.
const Two = 2

// More constants have a different header.
const (
	Three = 3
)

type T struct{}

func (*T) String() string { return "" }

// Check the implementation and define a default.
var (
	_ fmt.Stringer = (*T)(nil)

	// Default is collected while the assertion and block header stay here.
	Default = 1
)
