package r3

// Do no harm: the last signature line (final param + ") (returns) {") is
// irreducibly over the limit — re-packing the params can't fix it, so R3 must
// leave the already-multi-line signature alone instead of churning it.
func (m *mockNotifier) RegisterConfirmationsNtfn(txid *chainhash.Hash,
	_ []byte, numConfs, _ uint32,
	opts ...chainntnfs.NotifierOption) (*chainntnfs.ConfirmationEvent, error) {

	return nil, nil
}
