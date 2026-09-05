package r6

func test() {
	require.NoErrorWithAnUnnecessarilyLongName(t, tx.queries.UpsertOutput(ctx, sqlc.UpsertOutputParams{
		AccountCode: d.account,
	}))
}
