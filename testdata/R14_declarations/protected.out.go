package r14

const (
	Collected = 2
)

var (
	Before = run()
)

func first() {}

//noformat
const   Pinned = 1

//noformat
var   Middle=run()

var (
	After = run()
)

func second() {}

func run() int { return 1 }
