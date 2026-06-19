package r6

type Bip32Derivation struct {
	PubKey               []byte
	MasterKeyFingerprint uint32
	Bip32Path            []uint32
}

type Output struct {
	Bip32Derivation []*Bip32Derivation
}

func test(po *Output, keyData []byte, master uint32, derivationPath []uint32) {
	switch {
	default:
		switch {
		default:
			po.Bip32Derivation = append(
				po.Bip32Derivation, &Bip32Derivation{
					PubKey:               keyData,
					MasterKeyFingerprint: master,
					Bip32Path:            derivationPath,
				},
			)
		}
	}
}
