package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/format"
	"github.com/guggero/goformat/internal/syntax"
)

var (
	hunkHeader = regexp.MustCompile(
		`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`,
	)
)

// lineSpan is a zero-based, half-open interval. Empty spans describe an
// insertion point. Git's unchanged context lines are never included.
type lineSpan struct {
	start, end int
}

type lineEdit struct {
	old, new lineSpan
}

// gitLineEdits uses the same line diff algorithm for Git changes and formatter
// changes. Private temporary files prevent attributes or external diff drivers
// from changing the data being compared.
func gitLineEdits(before, after []byte) ([]lineEdit, error) {
	if bytes.Equal(before, after) {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "goformat-diff-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	oldPath, newPath := filepath.Join(
		dir, "before",
	), filepath.Join(
		dir, "after",
	)
	if err := os.WriteFile(oldPath, before, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(newPath, after, 0o600); err != nil {
		return nil, err
	}
	cmd := exec.Command(
		"git", "diff", "--no-index", "--no-ext-diff", "--no-textconv",
		"--no-color", "--text", "--unified=0", "--inter-hunk-context=0",
		"--diff-algorithm=myers", "--no-indent-heuristic", "--",
		oldPath, newPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	patch, err := cmd.Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); !ok ||
			failure.ExitCode() != 1 {

			return nil, fmt.Errorf("git diff: %w: %s", err, stderr.String())
		}
	}
	return parseLineEdits(patch)
}

func parseLineEdits(patch []byte) ([]lineEdit, error) {
	var edits []lineEdit
	for _, line := range strings.Split(string(patch), "\n") {
		if !strings.HasPrefix(line, "@@ ") {
			continue
		}
		match := hunkHeader.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("invalid Git hunk header: %q", line)
		}
		var spans [2]lineSpan
		for i := range spans {
			start, err := strconv.Atoi(match[1+i*2])
			if err != nil {
				return nil, err
			}
			count := 1
			if match[2+i*2] != "" {
				count, err = strconv.Atoi(match[2+i*2])
				if err != nil {
					return nil, err
				}
			}
			if count > 0 {
				start--
			}
			spans[i] = lineSpan{start, start + count}
		}
		edits = append(edits, lineEdit{spans[0], spans[1]})
	}
	return edits, nil
}

func sourceLines(src []byte) [][]byte {
	lines := bytes.SplitAfter(src, []byte("\n"))
	if len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func applyLineEdits(src, formatted []byte, edits []lineEdit) []byte {
	oldLines, newLines := sourceLines(src), sourceLines(formatted)
	var out bytes.Buffer
	previous := 0
	for _, edit := range edits {
		for _, line := range oldLines[previous:edit.old.start] {
			out.Write(line)
		}
		for _, line := range newLines[edit.new.start:edit.new.end] {
			out.Write(line)
		}
		previous = edit.old.end
	}
	for _, line := range oldLines[previous:] {
		out.Write(line)
	}
	return out.Bytes()
}

// formatChangedLines parses the entire file for context, but only accepts edits
// inside the selected syntax units. A second parse and syntax comparison reject
// incomplete transformations, such as half of a multi-line call wrap.
func formatChangedLines(src []byte, path string, cfg *config.Config,
	allowed []lineSpan) ([]byte, bool, error) {

	if len(allowed) == 0 {
		return src, false, nil
	}
	if len(allowed) == 1 && allowed[0].start == 0 &&
		allowed[0].end == len(sourceLines(src)) {

		out, _, err := format.Format(src, path, cfg)
		return out, false, err
	}

	// Collection moves declarations across the file and has no local form.
	// Whole-file additions above still receive all normal formatting rules.
	local := *cfg
	disabled := false
	local.Rules.DeclarationGrouping = &disabled
	formatted, _, err := format.Format(src, path, &local)
	if err != nil {
		return nil, false, err
	}
	edits, err := gitLineEdits(src, formatted)
	if err != nil {
		return nil, false, err
	}

	// Adjacent whitespace-only edits often arrive as one Git hunk. Split
	// equal-length replacements by line so an unrelated indentation fix
	// does not hide an otherwise independent edit. Syntax is checked below.
	var atomic []lineEdit
	for _, edit := range edits {
		if edit.old.end-edit.old.start == edit.new.end-edit.new.start {
			for i := 0; i < edit.old.end-edit.old.start; i++ {
				atomic = append(atomic, lineEdit{
					lineSpan{
						edit.old.start + i,
						edit.old.start + i + 1,
					},
					lineSpan{
						edit.new.start + i,
						edit.new.start + i + 1,
					},
				})
			}
		} else {
			atomic = append(atomic, edit)
		}
	}
	edits = atomic
	var selected []lineEdit
	groups := make([][]lineEdit, len(allowed))
	skipped := false
	for _, edit := range edits {
		for i, span := range allowed {
			if edit.old.start >= span.start &&
				edit.old.end <= span.end {

				selected = append(selected, edit)
				groups[i] = append(groups[i], edit)
				break
			}
			if edit.old.start < span.end &&
				edit.old.end > span.start {

				skipped = true
			}
		}
	}
	if len(selected) == 0 {
		return src, skipped, nil
	}
	original, err := syntax.Fingerprint(src)
	if err != nil {
		return nil, false, err
	}
	equivalent := func(candidate []byte) bool {
		fingerprint, err := syntax.Fingerprint(candidate)
		return err == nil && bytes.Equal(original, fingerprint)
	}
	candidate := applyLineEdits(src, formatted, selected)
	if equivalent(candidate) {
		return candidate, skipped, nil
	}

	// An incomplete fix in one hunk must not prevent an independent hunk
	// from being formatted. Keep only groups valid on their own.
	selected = nil
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		candidate = applyLineEdits(src, formatted, group)
		if equivalent(candidate) {
			selected = append(selected, group...)
		} else {
			skipped = true
		}
	}
	candidate = applyLineEdits(src, formatted, selected)
	if !equivalent(candidate) {
		return src, true, nil
	}
	return candidate, skipped, nil
}
