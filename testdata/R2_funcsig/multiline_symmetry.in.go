package r2

func main() {
	var addrs []*LongNameThatDoesntFit
	for _, addr := range update.Addresses {
		addrs = append(addrs,
			&LongNameThatDoesntFit{
				IdentityKey: update.IdentityKey,
				Address:     addr,
				ChainNet:    s.cfg.ActiveNetParams.Net,
			},
		)
	}
}

type LongNameThatDoesntFit struct {
	IdentityKey string
	Address     string
	ChainNet    uint
}
