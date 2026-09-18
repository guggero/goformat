package r9

// f exercises string reflow within a wrapped callback.
func f() {
	c, _ := newTestAuthClient(
		t, func(w http.ResponseWriter, r *http.Request) {
			require.Empty(
				t, r.Header.Get("Content-Type"),
				"a bodyless request must not claim to carry "+
					"JSON",
			)
		},
	)
}
