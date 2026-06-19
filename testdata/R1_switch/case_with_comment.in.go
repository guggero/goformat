package r1

func f(x int) {
	switch x {
	case 1:
		a()
	// describes case 2.
	case 2:
		b()
	}
}

func a() {}
func b() {}
