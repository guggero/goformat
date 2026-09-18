package main

import (
	"os"
	"strings"
	"testing"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/syntax"
	"github.com/stretchr/testify/require"
)

// TestChangedLinesRequireFormat permits the opt-in assertion rename only
// within the selected statement, while strict diff verification still sees it.
func TestChangedLinesRequireFormat(t *testing.T) {
	src, err := os.ReadFile(
		"../../testdata/R5_fmtfn/require_format.opt.in.go",
	)
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Optimize = true
	var allowed []lineSpan
	for i, line := range strings.Split(string(src), "\n") {
		if strings.Contains(line, `"session %s", name)`) &&
			strings.Contains(line, "require.Equal(") {

			allowed = append(allowed, lineSpan{i, i + 1})
		}
	}
	require.Len(t, allowed, 1)

	// Another eligible assertion remains outside the selected lines. The
	// syntax guard must accept the intended rename without pulling it in.
	out, skipped, err := formatChangedLines(src, "test.go", cfg, allowed)
	require.NoError(t, err)
	require.False(t, skipped)
	want := strings.Replace(
		string(src), "require.Equal(", "require.Equalf(", 1,
	)
	require.Equal(t, want, string(out))

	// --verify-diff uses the strict fingerprint, not the normalization
	// used to validate selected formatting edits. A rename is observable.
	before, err := syntax.Fingerprint(src)
	require.NoError(t, err)
	after, err := syntax.Fingerprint(out)
	require.NoError(t, err)
	require.NotEqual(t, string(before), string(after))
}
