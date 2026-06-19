package r4

func test(po *T, keyCode int, keyData, value []byte) error {
	if err := po.addUnknown(byte(keyCode), keyData, value, otherArg); err != nil {
		return err
	}
	return nil
}

type T struct{}

func (*T) addUnknown(a byte, b, c, d []byte) error { return nil }

var otherArg []byte
