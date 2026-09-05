package r14

func f() {}

type Mode uint8

// Mode values stay beside the type.
const (
	Off Mode = iota
	On
)

type Level int

const Lowest = Level(1)

type Alias = Level

const (
	Low Alias = 1
	High = Low + 1
)

type Unrelated string

const Other = 3

const (
	A = iota
	B
)

func g() {}

const (
	C = 10 + iota
	D
)

const Zero = iota

const (
	Plain = 2
	Repeated
)
