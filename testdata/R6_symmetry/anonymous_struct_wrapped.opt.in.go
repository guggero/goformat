package r6

func test() {
	execTplWithANameLongEnoughToRequireWrappingTheArgumentListHere(tpl, struct {
		CoinType uint32
		Account  uint32
	}{
		CoinType: coinType,
		Account:  uint32(account),
	})
}
