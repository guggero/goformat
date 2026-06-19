// Package diag defines the diagnostic type used by formatter passes to report
// issues that can't be fixed automatically (long lines that escape every wrap
// rule, structured-log calls with non-static msg, etc.).
package diag

import "fmt"

type Severity int

const (
	Info Severity = iota
	Warn
	Err
)

func (s Severity) String() string {
	switch s {
	case Info:
		return "info"

	case Warn:
		return "warn"

	case Err:
		return "error"
	}
	return "unknown"
}

type Diagnostic struct {
	Rule     string
	Severity Severity
	File     string
	Line     int
	Col      int
	Message  string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf(
		"%s:%d:%d: %s: [%s] %s",
		d.File, d.Line, d.Col, d.Severity, d.Rule, d.Message,
	)
}
