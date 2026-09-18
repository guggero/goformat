package r4

// f exercises nested calls.
func f() {
	repeat, err := api.authenticate(
		ctx, secondAddress, signCompactFor(t, secondAddress,
			liveChallenge(t, ctx, api, secondAddress)),
		walletName, first,
	)
}
