package r15

func foo() {
	switch {
	default:
		switch {
		default:
			// If the signature does not use SIGHASH_NONE, the
			// outputs are no longer modifiable. Clear bit 1. We
			// mask with 0x1f to ignore the ANYONECANPAY bit when
			// checking the base type.
			_ = 0
		}
	}
}
