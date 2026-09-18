package r5

import check "github.com/stretchr/testify/require"

// test exercises aliases and shadowed package names.
func test(checker any) {
	check.Equal(t, expected, actual, "value %v", actual)
	checker.Equal(t, expected, actual, "value %v", actual)
}

// shadow preserves selectors that belong to a local receiver.
func shadow(check any) {
	check.Equal(t, expected, actual, "value %v", actual)
}
