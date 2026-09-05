package r6

func test() {
	combine(&First{
		Value: first,
	}, argumentWhoseNameMakesTheSharedClosingAndOpeningLineTooLongToFit, &Second{
		Value: second,
	})
}
