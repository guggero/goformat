package r16

func test(nameBytes, templateLenVarInt, keysLenVarInt []byte) int {
	size := 1 + 1 + len(nameBytes) + len(templateLenVarInt) + 32 + len(keysLenVarInt) + 32
	return size
}
