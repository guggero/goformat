package r6

func test() {
	consume(tpl, &struct {
		Value string
	}{Value: "value"})
	consume(tpl, struct {
		Value string
	}{})
	consume(tpl, []struct {
		Value string
	}{
		{Value: "value"},
	})
	consume(tpl, map[string]struct {
		Value string
	}{
		"key": {Value: "value"},
	})
}
