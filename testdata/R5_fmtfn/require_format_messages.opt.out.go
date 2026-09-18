package r5

import "github.com/stretchr/testify/require"

// test recognizes literal message expressions without evaluating constants.
func test() {
	require.Equalf(t, want, got, "value %s", name)
	require.Equalf(t, want, got, `value %s`, name)
	require.Equalf(
		t, want, got, "value %[2]*.[1]*f", precision, width, value,
	)
	require.Equalf(t, want, got, "value %%%s", name)
	require.Equal(t, want, got, "value %%s", name)
	require.Equal(t, want, got, "value %", name)
}
