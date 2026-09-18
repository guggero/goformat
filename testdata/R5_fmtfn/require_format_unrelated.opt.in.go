package r5

import require "example.com/require"

// test leaves unrelated assertion libraries alone.
func test() {
	require.Equal(t, expected, actual, "session %s", name)
}
