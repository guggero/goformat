package r4

// Do no harm: the callee + "(" alone exceeds 80, so no argument wrapping can
// fix the opening line. R4 must leave the call's layout (here "})") untouched
// rather than churn the close to "},\n)".
func longSelectorCallee() {
	ctx.router.cfg.Payer.(*mockPaymentAttemptDispatcherOld).setPaymentResult(
		func(firstHop lnwire.ShortChannelID) ([32]byte, error) {
			return [32]byte{}, nil
		})
}
