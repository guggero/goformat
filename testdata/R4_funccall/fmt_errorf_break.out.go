package r4

import "fmt"

var (
	count int
)

func main() {
	err := fmt.Errorf("this is a long error message that we definitely "+
		"want %d", count)
	_ = err
}
