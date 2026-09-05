package r14

var (
	// Counters share a fixed-size array type.
	firstLongCounter, secondLongCounter, thirdLongCounter [4]int
	fourthLongCounter, fifthLongCounter                   [4]int
	keep                                                  = 1
)

func f() {}
