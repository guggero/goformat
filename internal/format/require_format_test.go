package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/syntax"
	"github.com/stretchr/testify/require"
)

// TestRequireFormatInvariants checks the readable fixtures for line overruns,
// unintended token changes, and formatting that changes again on a second run.
func TestRequireFormatInvariants(t *testing.T) {
	paths, err := filepath.Glob(
		"../../testdata/R5_fmtfn/require_format*.in.go",
	)
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			require.NoError(t, err)
			base := strings.TrimSuffix(
				filepath.Base(path), ".in.go",
			)
			cfg := cfgForPair(base)
			out, diagnostics, err := Format(src, path, cfg)
			require.NoError(t, err)
			require.Empty(t, diagnostics)

			// Renaming the recognized assertion is intentional. All
			// other syntax, including string values and comments,
			// must survive both conversion and compact formatting.
			normalized, err := OptimizeRequireCalls(src, cfg)
			require.NoError(t, err)
			before, err := syntax.Fingerprint(normalized)
			require.NoError(t, err)
			after, err := syntax.Fingerprint(out)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after))
			again, _, err := Format(out, path, cfg)
			require.NoError(t, err)
			require.Equal(t, string(out), string(again))
		})
	}
}

// TestRequireFormatOptOuts keeps the rename explicitly opt-in and governed by
// R5's configured function lists, independently of ordinary layout rewrites.
func TestRequireFormatOptOuts(t *testing.T) {
	src, err := os.ReadFile(
		"../../testdata/R5_fmtfn/require_format.opt.in.go",
	)
	require.NoError(t, err)
	for _, mode := range []string{"default", "disabled", "allow", "deny"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Default()
			cfg.Optimize = mode != "default"
			switch mode {
			case "disabled":
				require.NoError(t, cfg.SelectRules(
					[]string{
						"R4",
					},
				))

			case "allow":
				cfg.FormattingFuncs = []string{"fmt.Errorf"}

			case "deny":
				cfg.FormattingFuncsDeny = cfg.FormattingFuncs
			}

			// Compare the normalization directly: other passes may
			// still change layout, but none of these modes may
			// rename an assertion merely because its message has a
			// verb.
			out, err := OptimizeRequireCalls(src, cfg)
			require.NoError(t, err)
			require.Equal(t, string(src), string(out))
		})
	}
}

// TestPrintfVerb covers ordinary, indexed, and dynamic-width directives while
// distinguishing escaped percent signs and incomplete format strings.
func TestPrintfVerb(t *testing.T) {
	for message, want := range map[string]bool{
		"value %v":          true,
		"value %+v":         true,
		"value %08.2f":      true,
		"value %[2]s":       true,
		"value %[2]*.[1]*f": true,
		"value %%%s":        true,
		"100%%":             false,
		"value %%s":         false,
		"value %":           false,
		"plain message":     false,
	} {

		t.Run(message, func(t *testing.T) {
			require.Equal(t, want, hasPrintfVerb(message))
		})
	}
}
