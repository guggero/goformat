package format

import (
	"bytes"
	"strings"
	"testing"

	"github.com/guggero/goformat/internal/config"
)

func TestSelectedCallRules(t *testing.T) {
	src := `package p

func f() {
	ordinary(firstArgument, secondArgument, thirdArgument, fourthArgument)
	fmt.Errorf("this is a long format string that should split into several parts: %v", err)
	nested(
		items, func() bool {
			return true
		},
	)
}
`
	for _, id := range []string{"R4", "R5", "R6"} {
		t.Run(id, func(t *testing.T) {
			cfg := config.Default()
			cfg.LineLength = 60
			cfg.Optimize = true
			if err := cfg.SelectRules([]string{id}); err != nil {
				t.Fatal(err)
			}
			out, diagnostics, err := Format(
				[]byte(src), "p.go", cfg,
			)
			if err != nil || len(diagnostics) != 0 {
				t.Fatalf("format: %v %v", err, diagnostics)
			}
			got := string(out)
			ordinaryWrapped := strings.Contains(got, "ordinary(\n")
			formatSplit := strings.Contains(got, `"+`)
			symmetric := strings.Contains(
				got, "nested(items, func() bool {",
			)
			if ordinaryWrapped != (id == "R4") ||
				formatSplit != (id == "R5") ||
				symmetric != (id == "R6") {

				t.Fatalf("unrelated rule ran or selected rule "+
					"missed:\n%s", got)
			}
			again, _, err := Format(out, "p.go", cfg)
			if err != nil || !bytes.Equal(out, again) {
				t.Fatalf("not idempotent: %v\n%s", err, again)
			}
		})
	}
}

func TestSelectedRulesPreserveNoformat(t *testing.T) {
	src := []byte(
		`package p

//noformat
func untouched( ){println( 1 )}
`,
	)
	cfg := config.Default()
	if err := cfg.SelectRules([]string{
		"R3", "R12", "R14", "R15",
	}); err != nil {

		t.Fatal(err)
	}
	out, _, err := Format(src, "p.go", cfg)
	if err != nil || !bytes.Equal(src, out) {
		t.Fatalf("protected code changed: %v\n%s", err, out)
	}
}

func TestSelectedDiagnosticsProtection(t *testing.T) {
	src := []byte(
		`package p

//noformat
func protected() {
	log.InfoS(ctx, dynamicMessage)
}

//nolint
func ignored() {
	log.InfoS(ctx, dynamicMessage)
}

func checked() {
	log.InfoS(ctx, dynamicMessage)
}
`,
	)
	cfg := config.Default()
	if err := cfg.SelectRules([]string{"R8"}); err != nil {
		t.Fatal(err)
	}
	diagnostics, err := Diagnostics(src, "p.go", cfg)
	if err != nil || len(diagnostics) != 1 || diagnostics[0].Line != 14 {
		t.Fatalf("diagnostics: %v %v", err, diagnostics)
	}
}
