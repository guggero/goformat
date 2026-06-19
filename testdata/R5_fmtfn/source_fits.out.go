package r5

import "fmt"

func foo(idx int) error {
	switch {
	default:
		return fmt.Errorf("input %d has no UTXO information",
			idx)
	}
}
