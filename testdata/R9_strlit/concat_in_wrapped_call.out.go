package r9

import "github.com/stretchr/testify/require"

// Regression (lnd bench, idempotency): a string concat already living on its
// own continuation lines inside an ALREADY-wrapped call must not be re-split.
// The wrap indent must be counted once (not doubled), so the author's fitting
// 2-chunk split is recognized as fitting and left alone.
func concatInWrappedCall(t require.TestingT) {
	for i := 0; i < 3; i++ {
		select {
		case <-make(chan struct{}):

		default:
			require.Fail(
				t, "OnComplete callback timed out waiting "+
					"for execution after context cancel",
			)
		}
	}
}
