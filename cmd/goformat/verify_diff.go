package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/guggero/goformat/internal/syntax"
)

// verifyCommitDiff compares the net change between two explicit commit
// endpoints. It never reads file contents from the index or working tree.
func verifyCommitDiff(revisions string, stdout io.Writer) error {
	// Resolve each endpoint separately: rev-list range selection would make
	// a reversed ancestor range empty and could incorrectly attest success.
	left, right, ok := strings.Cut(revisions, "..")
	if !ok || left == "" || right == "" ||
		strings.Contains(right, "..") || strings.HasPrefix(right, ".") {

		return fmt.Errorf("invalid commit range %q: expected A..B "+
			"with two explicit endpoints (not A...B)", revisions)
	}
	var commits [2]string
	for i, ref := range []string{left, right} {
		resolved, err := gitOutput(
			".", "rev-parse", "--verify", "--end-of-options",
			ref+"^{commit}",
		)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", ref, err)
		}
		commits[i] = strings.TrimSpace(string(resolved))
	}

	// Include every path, even when launched from a subdirectory. Raw blob
	// IDs avoid revision/path ambiguity and remain valid for deleted files.
	// Disable rename detection and submodule hiding so metadata cannot be
	// silently discarded by the user's Git configuration.
	raw, err := gitOutput(
		".", "diff", "--raw", "--no-abbrev", "--no-renames", "-z",
		"--no-ext-diff", "--no-textconv", "--ignore-submodules=none",
		"--no-relative", commits[0], commits[1], "--",
	)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	entries := bytes.Split(raw, []byte{0})
	if len(entries)%2 != 1 || len(entries[len(entries)-1]) != 0 {
		return fmt.Errorf("incomplete Git raw diff records")
	}

	// Accumulate the complete report before publishing it. A parse or Git
	// error must not leave a partial diff that looks like a completed
	// check.
	var report bytes.Buffer
	for i := 0; i < len(entries)-1; i += 2 {
		meta := strings.Fields(string(entries[i]))
		if len(meta) != 5 || !strings.HasPrefix(meta[0], ":") {
			return fmt.Errorf("invalid Git raw diff record %q",
				entries[i])
		}
		name := string(entries[i+1])
		oldMode := strings.TrimPrefix(meta[0], ":")
		regular := oldMode == "100644" || oldMode == "100755"
		if meta[4] != "M" || oldMode != meta[1] || !regular ||
			!strings.HasSuffix(name, ".go") {

			// AST equivalence cannot certify file existence, modes,
			// symlinks, submodule pointers, or another language.
			fmt.Fprintf(&report, "requires review: %q: status %s; "+
				"addition/deletion, mode/type, or non-Go change\n",
				name, meta[4])
			patch, err := gitOutput(
				".", "diff", "--binary", "--no-color",
				"--no-ext-diff", "--no-textconv",
				"--no-renames", "--ignore-submodules=none",
				"--no-relative", "--src-prefix=a/",
				"--dst-prefix=b/", commits[0], commits[1], "--",
				":(top,literal)"+name,
			)
			if err != nil {
				return err
			}
			report.Write(patch)
			continue
		}

		// Use the shared structural checker, not the formatter: running
		// the formatting passes here could repeat and hide the same
		// bug.
		var snapshots [2][]byte
		for side, blob := range meta[2:4] {
			src, err := gitOutput(".", "cat-file", "blob", blob)
			if err != nil {
				return err
			}
			snapshots[side], err = syntax.Fingerprint(src)
			if err != nil {
				return fmt.Errorf("parse %q at %s: %w", name,
					commits[side], err)
			}
		}
		if bytes.Equal(snapshots[0], snapshots[1]) {
			continue
		}
		if err := writeSyntaxDiff(
			&report, name, snapshots,
		); err != nil {

			return err
		}
	}
	if report.Len() == 0 {
		return nil
	}
	if _, err := stdout.Write(report.Bytes()); err != nil {
		return err
	}
	return errChangesNeeded
}

// writeSyntaxDiff emits every changed structural line with zero context. Line
// numbers refer to canonical JSON, so source wrapping never pollutes the diff.
func writeSyntaxDiff(w io.Writer, name string, snapshots [2][]byte) error {
	var text [2][]byte
	for i, snapshot := range snapshots {
		var err error
		text[i], err = readableFingerprint(snapshot)
		if err != nil {
			return err
		}
	}
	edits, err := gitLineEdits(text[0], text[1])
	if err != nil {
		return err
	}
	if len(edits) == 0 {
		return fmt.Errorf("different syntax for %q produced no diff", name)
	}

	// Keep filenames quoted so embedded newlines cannot look like another
	// file or hunk. The entire report is buffered by the caller.
	var patch bytes.Buffer
	fmt.Fprintf(&patch, "--- %q (canonical AST)\n", "a/"+name)
	fmt.Fprintf(&patch, "+++ %q (canonical AST)\n", "b/"+name)
	before, after := sourceLines(text[0]), sourceLines(text[1])
	for _, edit := range edits {
		fmt.Fprintf(&patch, "@@ -%s +%s @@\n",
			verificationRange(
				edit.old,
			), verificationRange(
				edit.new,
			))
		for _, line := range before[edit.old.start:edit.old.end] {
			patch.WriteByte('-')
			patch.Write(line)
		}
		for _, line := range after[edit.new.start:edit.new.end] {
			patch.WriteByte('+')
			patch.Write(line)
		}
	}
	_, err = w.Write(patch.Bytes())
	return err
}

// verificationRange converts a half-open line span to unified-diff coordinates.
// Empty spans point between lines rather than at the next existing line.
func verificationRange(span lineSpan) string {
	start, count := span.start, span.end-span.start
	if count > 0 {
		start++
	}
	return fmt.Sprintf("%d,%d", start, count)
}

// readableFingerprint labels the checker's sections and renders string bytes
// as quoted Go strings. Escaping preserves invalid UTF-8 and significant spaces
// while making a changed literal more useful to a reader than base64.
func readableFingerprint(snapshot []byte) ([]byte, error) {
	var sections []any
	if err := json.Unmarshal(snapshot, &sections); err != nil {
		return nil, err
	}
	if len(sections) != 4 {
		return nil, fmt.Errorf("unexpected syntax fingerprint sections")
	}
	decodeFingerprintStrings(sections[0])
	text, err := json.MarshalIndent(map[string]any{
		"syntax": sections[0], "comments": sections[1],
		"directives": sections[2], "cgo": sections[3],
	}, "", "  ")
	return append(text, '\n'), err
}

// decodeFingerprintStrings changes only the tagged string-literal values in
// the display copy; comparison still uses the original byte-safe fingerprint.
func decodeFingerprintStrings(value any) {
	switch value := value.(type) {
	case []any:
		if len(value) == 2 && value[0] == "string" {
			if encoded, ok := value[1].(string); ok {
				decoded, err := base64.StdEncoding.DecodeString(
					encoded,
				)
				if err == nil {
					value[1] = strconv.Quote(string(
						decoded,
					))
				}
			}
			return
		}
		for _, child := range value {
			decodeFingerprintStrings(child)
		}

	case map[string]any:
		for _, child := range value {
			decodeFingerprintStrings(child)
		}
	}
}
