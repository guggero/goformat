package r15

// Regression (lnd bench): "//nolint" (no colon) and "///" banner comments must
// be left verbatim — R15 was inserting a space ("// nolint", "// /"), which
// also breaks the nolint linter directive.
func directiveComments() {
	cltvs := 0
	cltvs = appendThing(cltvs) //nolint
	_ = cltvs

	/// Restart nursery.
	doRestart()
}
