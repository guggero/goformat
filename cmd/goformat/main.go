// Command goformat is a configurable Go formatter that extends gofmt with the
// rules from code-ingest/development_guidelines.md (lnd style).
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/format"
)

const usage = `usage: goformat [flags] [path...]

Modes (exclusive; default: -d):
  -w              rewrite files in place
  -d              print before/after on changed files (default)
  -l              list files that would change
  -check          exit non-zero if any changes needed

Config:
  -config FILE    explicit config file (else search upward for goformat.toml)
  -no-config      ignore config files, use defaults

Info:
  -explain RULE   print a short reference for a rule (e.g. -explain R3)
                  with no RULE, lists every rule
  -rules          alias for -explain (no argument)

paths: file or directory (recursed); "-" reads stdin.
`

type mode int

const (
	modeDiff mode = iota
	modeWrite
	modeList
	modeCheck
)

var errChangesNeeded = errors.New("changes needed")

func main() {
	err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	switch {
	case err == nil:
		return

	case errors.Is(err, errChangesNeeded):
		os.Exit(1)

	default:
		fmt.Fprintln(os.Stderr, "goformat:", err)
		os.Exit(2)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("goformat", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() { fmt.Fprint(stderr, usage) }

	var (
		write = fset.Bool("w", false, "write files in place")
		diff  = fset.Bool(
			"d", false, "print before/after on changed files",
		)
		list  = fset.Bool("l", false, "list files that would change")
		check = fset.Bool(
			"check", false, "exit non-zero on any pending change",
		)
		cfgPath  = fset.String("config", "", "config file path")
		noConfig = fset.Bool("no-config", false, "ignore config files")
		explain  = fset.String(
			"explain", "", "print reference for a rule (e.g. R3)",
		)
		rules    = fset.Bool("rules", false, "list every rule and exit")
		optimize = fset.Bool(
			"optimize", false, "also apply soft, space-efficiency "+
				"fixes to code that already fits",
		)
	)
	if err := fset.Parse(args); err != nil {
		return err
	}

	if *rules ||
		(*explain == "" && hasFlag(args, "-explain", "--explain")) {

		writeAllRules(stdout)
		return nil
	}
	if *explain != "" {
		return writeRuleExplanation(stdout, *explain)
	}

	m, err := resolveMode(*write, *diff, *list, *check)
	if err != nil {
		return err
	}

	cfg, err := resolveConfig(*cfgPath, *noConfig)
	if err != nil {
		return err
	}
	if *optimize {
		cfg.Optimize = true
	}

	paths := fset.Args()
	if len(paths) == 0 {
		paths = []string{"-"}
	}

	anyChanged := false
	for _, p := range paths {
		changed, err := processPath(p, m, cfg, stdin, stdout)
		if err != nil {
			return err
		}
		anyChanged = anyChanged || changed
	}
	if m == modeCheck && anyChanged {
		return errChangesNeeded
	}
	return nil
}

func resolveMode(write, diff, list, check bool) (mode, error) {
	count := 0
	for _, b := range []bool{write, diff, list, check} {
		if b {
			count++
		}
	}
	if count > 1 {
		return modeDiff, errors.New(
			"only one of -w, -d, -l, -check may be set",
		)
	}
	switch {
	case write:
		return modeWrite, nil

	case list:
		return modeList, nil

	case check:
		return modeCheck, nil

	default:
		return modeDiff, nil
	}
}

func resolveConfig(cfgPath string, noConfig bool) (*config.Config, error) {
	if noConfig {
		return config.Default(), nil
	}
	path := cfgPath
	if path == "" {
		cwd, _ := os.Getwd()
		path = config.Find(cwd)
	}
	return config.Load(path)
}

func processPath(p string, m mode, cfg *config.Config, stdin io.Reader,
	stdout io.Writer) (bool, error) {

	if p == "-" {
		return processStdin(m, cfg, stdin, stdout)
	}
	info, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return processFile(p, m, cfg, stdout)
	}
	return processDir(p, m, cfg, stdout)
}

func processStdin(m mode, cfg *config.Config, stdin io.Reader,
	stdout io.Writer) (bool, error) {

	src, err := io.ReadAll(stdin)
	if err != nil {
		return false, err
	}
	out, _, err := format.Format(src, "<stdin>", cfg)
	if err != nil {
		return false, err
	}
	changed := !bytes.Equal(src, out)
	switch m {
	case modeWrite:
		_, werr := stdout.Write(out)
		return changed, werr

	case modeDiff:
		if changed {
			_, _ = fmt.Fprint(
				stdout, briefDiff("<stdin>", src, out),
			)
		}

	case modeList:
		if changed {
			_, _ = fmt.Fprintln(stdout, "<stdin>")
		}

	case modeCheck:
		// signal only via return value
	}
	return changed, nil
}

func processFile(path string, m mode, cfg *config.Config,
	stdout io.Writer) (bool, error) {

	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	out, _, err := format.Format(src, path, cfg)
	if err != nil {
		return false, err
	}
	if bytes.Equal(src, out) {
		return false, nil
	}
	switch m {
	case modeWrite:
		return true, os.WriteFile(path, out, 0o644)

	case modeDiff:
		_, _ = fmt.Fprint(stdout, briefDiff(path, src, out))

	case modeList:
		_, _ = fmt.Fprintln(stdout, path)

	case modeCheck:
		// signal only
	}
	return true, nil
}

func processDir(root string, m mode, cfg *config.Config,
	stdout io.Writer) (bool, error) {

	anyChanged := false
	err := filepath.WalkDir(
		root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path == root {
					return nil
				}
				name := d.Name()
				if name == "vendor" || name == "testdata" ||
					strings.HasPrefix(name, ".") ||
					strings.HasPrefix(name, "_") {

					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") {
				return nil
			}
			changed, err := processFile(path, m, cfg, stdout)
			if err != nil {
				return err
			}
			anyChanged = anyChanged || changed
			return nil
		},
	)
	return anyChanged, err
}

func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n {
				return true
			}
		}
	}
	return false
}

func writeAllRules(w io.Writer) {
	for _, r := range format.Rules() {
		fmt.Fprintf(w, "%-4s %s\n", r.ID, r.Title)
	}
	fmt.Fprintln(
		w, "\nRun 'goformat -explain RULE' for a longer description.",
	)
}

func writeRuleExplanation(w io.Writer, id string) error {
	r := format.ExplainRule(id)
	if r == nil {
		return fmt.Errorf("unknown rule %q (try -rules to list)", id)
	}
	fmt.Fprintf(w, "%s — %s\n\n%s\n", r.ID, r.Title, r.Summary)
	return nil
}

// briefDiff prints the first 5 differing lines on each side, with a "(n more
// lines differ)" tail. Phase 5 keeps the renderer naïve to avoid a
// unified-diff dependency; -w/-l/-check cover the production paths.
func briefDiff(name string, a, b []byte) string {
	aLines := strings.Split(string(a), "\n")
	bLines := strings.Split(string(b), "\n")

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", name)
	max := len(aLines)
	if len(bLines) > max {
		max = len(bLines)
	}
	shown, diffs := 0, 0
	for i := 0; i < max; i++ {
		var av, bv string
		if i < len(aLines) {
			av = aLines[i]
		}
		if i < len(bLines) {
			bv = bLines[i]
		}
		if av == bv {
			continue
		}
		diffs++
		if shown < 5 {
			if i < len(aLines) {
				fmt.Fprintf(&sb, "-%4d: %s\n", i+1, av)
			}
			if i < len(bLines) {
				fmt.Fprintf(&sb, "+%4d: %s\n", i+1, bv)
			}
			shown++
		}
	}
	if diffs > shown {
		fmt.Fprintf(&sb, "(%d more lines differ)\n", diffs-shown)
	}
	return sb.String()
}
