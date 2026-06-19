package r2

import "errors"

// NewV2 creates a new PSBTv2 Packet.
func NewV2(txVersion int32, fallbackLocktime *uint32,
	txModifiable *uint8) (*Packet, error) {

	if txVersion < 2 {
		return nil, errors.New("foo")
	}

	// For V2, UnsignedTx must be nil and TxVersion is explicitly required.
	return &Packet{}, nil
}

type Packet struct{}
