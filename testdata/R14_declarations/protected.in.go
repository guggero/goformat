package r14

func first() {}

//noformat
const   Pinned = 1

const Collected = 2

var Before = run()

//noformat
var   Middle=run()

func second() {}

var After = run()

func run() int { return 1 }
