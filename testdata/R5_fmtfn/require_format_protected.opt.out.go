package r5

import "github.com/stretchr/testify/require"

// test keeps protected assertion calls intact.
func test() {
	//noformat
	require.Equal(t, expected, actual, "session %s", name)
}

//nolint
func ignored() {
	require.Equal(t, expected, actual, "session %s", name)
}
