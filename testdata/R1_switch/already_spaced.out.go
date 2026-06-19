package r1

func f(x int) {
	switch x {
	case 1:
		a()

	case 2:
		b()

	default:
		c()
	}
}

func a() {}
func b() {}
func c() {}
