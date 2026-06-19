package r2

func test(po *T, keyCode int, keyData, value []byte) error {
	switch {
	default:
		if keyData != nil {
			if err := po.addUnknown(
				byte(keyCode), keyData, value,
			); err != nil {

				return err
			}
		}
	}
	return nil
}

type T struct{}

func (*T) addUnknown(a byte, b, c []byte) error { return nil }
