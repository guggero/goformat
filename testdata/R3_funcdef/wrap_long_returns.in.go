package r3

// Wrapping the results brings the final signature line within the limit.
func (m *mockNotifier) RegisterConfirmationsNtfn(txid *chainhash.Hash,
	_ []byte, numConfs, _ uint32,
	opts ...chainntnfs.NotifierOption) (*chainntnfs.ConfirmationEvent, error) {

	return nil, nil
}
