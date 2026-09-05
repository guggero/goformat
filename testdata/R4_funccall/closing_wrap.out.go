package r4

func test() {
	outer(
		first, inner(second, third),
	)
	call(first, second)
}
