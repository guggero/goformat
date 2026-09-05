package r3

func test(t *testing.T) {
	t.Run(
		"unsupported currency never hits the source",
		func(t *testing.T) {
			t.Helper()
		},
	)
}
