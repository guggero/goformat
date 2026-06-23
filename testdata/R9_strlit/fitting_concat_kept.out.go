package r9

// Cluster A (lnd bench): an existing multi-line string concat whose every line
// already fits within the limit must be left untouched. goformat used to
// re-split such a fitting 2-chunk concat into 3 chunks (churn, strictly worse).
func fittingConcatKept() string {
	msg := "OnComplete callback timed out waiting " +
		"for execution after context cancel"

	return msg
}
