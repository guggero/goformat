package r6

func test() {
	require.NoError(t, tx.queries.UpsertOutput(ctx, sqlc.UpsertOutputParams{
		AccountCode:   d.account,
		OutPointHash:  txA,
		OutPointIndex: 0,
		Value:         sql.NullInt64{Int64: 1000, Valid: true},
		PkScript:      pkScript,
	}))
	require.NoError(t, tx.queries.UpsertInput(ctx, sqlc.UpsertInputParams{
		AccountCode:    d.account,
		PrevOutHash:    txA,
		PrevOutIndex:   0,
		SpendingTxHash: txB,
	}))
}
