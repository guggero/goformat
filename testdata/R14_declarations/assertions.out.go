package r14

var (
	// Ordinary should be collected.
	Ordinary = 1
)

type I interface{ f() }

type T struct{}

func (*T) f() {}

var (
	// Keep this interface assertion beside the implementation.
	_ I = (*T)(nil)

	// Keep the other assertion too.
	_ I = &T{}
)

var _ I = &T{}
