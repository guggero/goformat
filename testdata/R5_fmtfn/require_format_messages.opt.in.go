package r5

import "github.com/stretchr/testify/require"

// test recognizes literal message expressions without evaluating constants.
func test() {
	require.Equal(t, want, got, "value %"+"s", name)
	require.Equal(t, want, got, `value %s`, name)
	require.Equal(t, want, got, "value %[2]*.[1]*f", precision, width, value)
	require.Equal(t, want, got, "value %%%s", name)
	require.Equal(t, want, got, "value %%s", name)
	require.Equal(t, want, got, "value %", name)
}
