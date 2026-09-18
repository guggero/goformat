package r5

import "github.com/stretchr/testify/require"

// test exercises the compact exception after assertion operands.
func test() {
	require.Equal(t, expected, actual, "the completed session should use the stored token for user %s", name)
}
