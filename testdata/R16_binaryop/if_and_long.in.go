package r16

import "strings"

func test(ch rune) bool {
	for i := 0; i < 10; i++ {
		if strings.ContainsRune("abc", ch) && !strings.ContainsRune("def", ch) {
			return true
		}
	}
	return false
}
