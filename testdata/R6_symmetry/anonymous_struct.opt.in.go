package r6

func test() {
	path, err := execTpl(tpl, struct {
		CoinType uint32
		Account  uint32
	}{
		CoinType: coinType,
		Account:  uint32(account),
	})
	_, _ = path, err
}
