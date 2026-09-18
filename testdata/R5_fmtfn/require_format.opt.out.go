package r5

import "github.com/stretchr/testify/require"

// test exercises message-aware assertion conversion.
func test() {
	require.Equalf(t, expected, actual, "session %s", name)
	require.NoErrorf(t, err, "session %[1]s", name)
	require.Contains(t, value, "%s", "plain message")
	require.Equal(t, "%s", actual, "plain message", name)
	require.Equal(t, expected, actual, "no value for %s")
	require.Equal(t, expected, actual, "100%% complete", name)
	require.Equal(t, expected, actual, dynamic, name)
	require.Equal(t, expected, actual, args...)
	require.Equalf(t, expected, actual, "session %s", name)
}
