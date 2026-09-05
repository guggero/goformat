package r6

func test() {
	require.NoError(t, wrap(save(ctx, &Params{
		Value: value,
	})))
	check(t, wrap(save(ctx, &Params{
		Value: value,
	})), expected)
	require.NoError(t, run(ctx, func() error {
		return nil
	}))
}
