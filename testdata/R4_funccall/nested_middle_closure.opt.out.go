package r4

func test() {
	require.NoError(t, checkReady(
		t, func() bool {
			return ready()
		}, harness.SweepArrivalTimeout, harness.NormalPollInterval,
		"destination address never received the exit funds",
	))
}
