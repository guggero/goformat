package r4

func test() {
	require.Eventually(
		t,
		func(parameterxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx bool) bool {
			return true
		}, harness.SweepArrivalTimeout, harness.NormalPollInterval,
		"destination address never received the exit funds",
	)
}
