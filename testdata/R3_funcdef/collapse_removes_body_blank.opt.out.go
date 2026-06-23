package r3

type T struct {
	X int
}

func (u *T) Method(arg []byte, idx int) error {
	u.X = idx
	return nil
}
