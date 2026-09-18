package r6

// test exercises the line-count preference for valid calls.
func test() {
	status, err := broker.CompleteSession(ctx,
		completeSession(fake.loginCode))
}
