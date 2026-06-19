package r4

import (
	"encoding/binary"
)

type T struct {
	txVersion int32
}

func test(value []byte) {
	var res T
	switch {
	default:
		switch {
		default:
			switch {
			default:
				res.txVersion = int32(
					binary.LittleEndian.Uint32(value),
				)
			}
		}
	}
	_ = res
}
