package r4

func test() {
	for _, d := range derivations {
		require.False(t, dup,
			"derivation %v reused the address of %v", d, prev)
	}
	require.NotEqual(t,
		newAddress(t, 0, d).EncodeForHumans(),
		newAddress(t, 1, d).EncodeForHumans(),
	)
}
