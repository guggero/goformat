package r4

func test() {
	pkScript := append(
		[]byte{
			0x00, 0x14,
		}, bytes.Repeat([]byte{
			0x01,
		}, 20)...)
}
