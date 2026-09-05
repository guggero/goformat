package r15

var (
	// short comment stays alone
	x int
)

// TestConstructorAddInputV2RespectsTxModifiable verifies that the
// Constructor-role checks modifiable flags before mutating the packet.
func TestConstructorAddInputV2RespectsTxModifiable() {}

//go:noinline
func keepDirective() {}
