package r2

import (
	"encoding/binary"
	"sort"
)

const (
	uint32Size = 4
)

func main() {
	var pi X
	sort.Slice(pi.TaprootScriptSpendSig, func(i, j int) bool {
		return pi.TaprootScriptSpendSig[i].SortBefore(
			pi.TaprootScriptSpendSig[j],
		)
	})

	var (
		path  []byte
		paths []uint32
	)
	for i := uint32Size; i < len(path); i += uint32Size {
		paths = append(paths, binary.LittleEndian.Uint32(
			path[i:i+uint32Size],
		))
		paths = append(paths, binary.LittleEndian.Uint32(
			path[i:i+uint32Size],
		))
	}

	var addrs []*LongNameThatDoesntFit
	for _, addr := range update.Addresses {
		addrs = append(addrs, &LongNameThatDoesntFit{
			IdentityKey: update.IdentityKey,
			Address:     addr,
			ChainNet:    s.cfg.ActiveNetParams.Net,
		})
	}
}

type X struct {
	TaprootScriptSpendSig []*S
}

type S struct {
}

func (s *S) SortBefore(o *S) bool {
	return true
}

type LongNameThatDoesntFit struct {
	IdentityKey string
	Address     string
	ChainNet    uint
}
