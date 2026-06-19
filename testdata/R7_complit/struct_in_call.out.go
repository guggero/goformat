package r7

type PInput struct {
	PreviousTxid []byte
	OutputIndex  uint32
}

type Packet struct {
	Inputs []PInput
}

func testTxid(b byte) []byte { return nil }

func test(p *Packet) {
	switch {
	default:
		switch {
		default:
			p.Inputs = append(p.Inputs, PInput{
				PreviousTxid: testTxid(0x01)[:],
				OutputIndex:  0,
			})
		}
	}
}
