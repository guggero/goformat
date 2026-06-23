package r4

// A //nolint:ll line is intentionally over-limit; R4 must not re-wrap the
// surrounding call on its account (the other args already fit).
func nolintNoRewrap() {
	adapters.On(
		"RegisterConfirmationsNtfn",
		expectedTxid, expectedPkScript,
		mock.MatchedBy(func(opts []chainntnfs.NotifierOption) bool { //nolint:ll
			return len(opts) == 1
		}),
	)
}
