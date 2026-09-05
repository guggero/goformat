package r14

type I interface{ f() }

type T struct{}

func (*T) f() {}

var (
	// Keep this interface assertion beside the implementation.
	_ I = (*T)(nil)

	// Ordinary should be collected.
	Ordinary = 1

	// Keep the other assertion too.
	_ I = &T{}
)

var _ I = &T{}
