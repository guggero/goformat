package r4

func test() {
	ordinary(
		first,
		second,
	)
	outer(a, inner(
		first,
		second,
	))
	outer(a, save(ctx, &Params{
		Value: value,
	}))
	outer(
		a, &Params{
			Value: value,
		},
	)
	fmt.Printf("first %v second %v",
		first, second)
	log.InfoS(ctx, "message",
		slog.String("first", first),
		slog.String("second", second))
}
