package r4

func test() {
	require.Eventually(
		t,
		func() bool {
			return ready()
		},
		harness.SweepArrivalTimeout, harness.NormalPollInterval,
		"destination address never received the exit funds",
	)
}
