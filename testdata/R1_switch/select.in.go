package r1

func f(c1, c2 <-chan int, done <-chan struct{}) {
	select {
	case v := <-c1:
		_ = v
	case v := <-c2:
		_ = v
	case <-done:
		return
	}
}
