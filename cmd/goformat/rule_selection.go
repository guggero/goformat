package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/diag"
	"github.com/guggero/goformat/internal/format"
)

// ruleFlag accumulates rule IDs without dropping empty entries: a typo such
// as --rule R3, should produce an error rather than silently select R3.
type ruleFlag []string

func (r *ruleFlag) String() string { return strings.Join(*r, ",") }

func (r *ruleFlag) Set(value string) error {
	*r = append(*r, strings.Split(value, ",")...)
	return nil
}

// reportRuleDiagnostics makes explicitly selected lint rules visible to the
// CLI. A nil scope includes the entire file; an empty scope includes nothing.
func reportRuleDiagnostics(diagnostics []diag.Diagnostic, cfg *config.Config,
	stderr io.Writer, scopes []lineSpan) bool {

	if len(cfg.SelectedRules) == 0 {
		return false
	}
	reported := false
	for _, d := range diagnostics {
		included := scopes == nil
		for _, span := range scopes {
			included = included ||
				(d.Line-1 >= span.start && d.Line-1 < span.end)
		}
		if included {
			fmt.Fprintln(stderr, d.String())
			reported = true
		}
	}
	return reported
}

// reportChangedRuleDiagnostics restricts selected lint rules to the same
// affected constructs as formatting, using the final working-tree positions.
func reportChangedRuleDiagnostics(baseline, out []byte, name string,
	cfg *config.Config, stderr io.Writer) (bool, error) {

	if len(cfg.SelectedRules) == 0 || (!cfg.Rules.LineLengthCheckOn() &&
		!cfg.Rules.StructuredLogWrapOn()) {

		return false, nil
	}
	hunks, err := gitLineEdits(baseline, out)
	if err != nil {
		return false, err
	}
	scopes, err := formattingScopes(out, changedSpans(hunks))
	if err != nil {
		return false, err
	}
	if len(scopes) == 0 {
		return false, nil
	}
	diagnostics, err := format.Diagnostics(out, name, cfg)
	if err != nil {
		return false, err
	}
	return reportRuleDiagnostics(diagnostics, cfg, stderr, scopes), nil
}
