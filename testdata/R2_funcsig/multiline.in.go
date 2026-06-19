package r2

func deriveRevocationKeyWithExtras(commitPubKey []byte,
	revokePreimage []byte) ([]byte, error) {
	out := append([]byte{}, commitPubKey...)
	return append(out, revokePreimage...), nil
}
