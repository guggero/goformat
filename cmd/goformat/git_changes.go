package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/format"
)

type changeScope struct {
	path      string
	directory bool
}

type gitChange struct {
	path, original string
	conflicted     bool
}

func gitOutput(root string, args ...string) ([]byte, error) {
	command := append([]string{"--no-optional-locks", "-C", root}, args...)
	cmd := exec.Command("git", command...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// changedScopes resolves each explicit path in its own repository. With no
// paths, the current directory is the scope, matching ordinary directory use.
func changedScopes(paths []string) (map[string][]changeScope, error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}
	repos := make(map[string][]changeScope)
	for _, path := range paths {
		if path == "-" {
			return nil, fmt.Errorf("Git change flags cannot be " +
				"used with stdin")
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(absolute)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("Git change flags do not "+
				"follow symlink %q", path)
		}
		absolute, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, err
		}
		dir := absolute
		if !info.IsDir() {
			dir = filepath.Dir(absolute)
		}
		out, err := gitOutput(dir, "rev-parse", "--show-toplevel")
		if err != nil {
			return nil, fmt.Errorf("%s: Git change flags require "+
				"a working tree: %w", path, err)
		}
		root, err := filepath.EvalSymlinks(strings.TrimSuffix(
			string(out), "\n",
		))
		if err != nil {
			return nil, err
		}
		repos[root] = append(repos[root], changeScope{
			absolute, info.IsDir(),
		})
	}
	return repos, nil
}

func selectedChange(path string, scopes []changeScope,
	cfg *config.Config) bool {

	if filepath.Ext(path) != ".go" {
		return false
	}
	for _, scope := range scopes {
		if !scope.directory {
			if scope.path == path {
				return true
			}
			continue
		}
		rel, err := filepath.Rel(scope.path, path)
		if err != nil || rel == ".." || strings.HasPrefix(
			rel, ".."+string(filepath.Separator),
		) {

			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		candidate := scope.path
		excluded := false
		for i, part := range parts {
			candidate = filepath.Join(candidate, part)
			if i < len(
				parts,
			)-1 && (part == "vendor" || part == "testdata" ||
				strings.HasPrefix(
					part, ".",
				) || strings.HasPrefix(part, "_")) {

				excluded = true
				break
			}
			if matchExclude(
				cfg.Exclude, scope.path, candidate, part,
			) {

				excluded = true
				break
			}
		}
		if !excluded {
			return true
		}
	}
	return false
}

func repositoryChanges(root string, uncommitted bool) ([]gitChange, error) {
	unborn := false
	if uncommitted {
		// show-ref distinguishes an unborn HEAD from failures to read
		// Git state without requiring or creating an empty-tree object.
		_, err := gitOutput(
			root, "rev-parse", "--verify", "--quiet", "HEAD",
		)
		if err != nil {
			if _, verifyErr := gitOutput(
				root, "symbolic-ref", "-q", "HEAD",
			); verifyErr != nil {

				return nil, err
			}
			unborn = true
		}
	}
	changes := make(map[string]gitChange)
	if !unborn {
		args := []string{"diff", "--raw", "-z", "--no-abbrev",
			"--no-ext-diff", "--no-textconv", "--find-renames",
			"--ignore-submodules=all"}
		if uncommitted {
			args = append(args, "HEAD")
		}
		args = append(args, "--")
		raw, err := gitOutput(root, args...)
		if err != nil {
			return nil, err
		}
		parsed, err := parseGitChanges(raw)
		if err != nil {
			return nil, err
		}
		for _, change := range parsed {
			changes[change.path] = change
		}
	}
	args := []string{"ls-files", "--others", "--exclude-standard", "-z"}
	if unborn {
		args = append(args, "--cached")
	}
	untracked, err := gitOutput(root, args...)
	if err != nil {
		return nil, err
	}
	for _, path := range strings.Split(string(untracked), "\x00") {
		if path != "" {
			changes[path] = gitChange{path: path}
		}
	}
	conflicts, err := gitOutput(
		root, "diff", "--name-only", "--diff-filter=U", "-z", "--",
	)
	if err != nil {
		return nil, err
	}
	for _, path := range strings.Split(string(conflicts), "\x00") {
		if change, ok := changes[path]; ok {
			change.conflicted = true
			changes[path] = change
		}
	}
	var result []gitChange
	for _, change := range changes {
		result = append(result, change)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].path < result[j].path
	})
	return result, nil
}

// Raw -z output uses NUL-separated names, so spaces, tabs, quotes, and newlines
// cannot be mistaken for patch syntax. Renames retain their original blob as
// the baseline; renaming alone does not make the entire file eligible.
func parseGitChanges(raw []byte) ([]gitChange, error) {
	parts := strings.Split(string(raw), "\x00")
	var changes []gitChange
	for i := 0; i < len(parts)-1; {
		fields := strings.Fields(parts[i])
		if len(fields) != 5 || !strings.HasPrefix(fields[0], ":") ||
			i+1 >= len(parts)-1 {

			return nil, fmt.Errorf("invalid Git raw diff record "+
				"%q", parts[i])
		}
		status := fields[4]
		path := parts[i+1]
		i += 2
		if strings.HasPrefix(status, "R") ||
			strings.HasPrefix(status, "C") {

			if i >= len(parts)-1 {
				return nil, fmt.Errorf("missing Git rename " +
					"destination")
			}
			path = parts[i]
			i++
		}
		if fields[1] != "100644" && fields[1] != "100755" &&
			status != "U" {

			continue
		}
		original := fields[2]
		if strings.Trim(original, "0") == "" {
			original = ""
		}
		changes = append(changes, gitChange{
			path, original, status == "U",
		})
	}
	return changes, nil
}

func processChangedPaths(paths []string, uncommitted bool, m mode,
	cfg *config.Config, stdout, stderr io.Writer) error {

	repos, err := changedScopes(paths)
	if err != nil {
		return err
	}
	var roots []string
	for root := range repos {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	anyChanged := false
	for _, root := range roots {
		changes, err := repositoryChanges(root, uncommitted)
		if err != nil {
			return err
		}
		for _, change := range changes {
			path := filepath.Join(root, filepath.FromSlash(
				change.path,
			))
			if !selectedChange(path, repos[root], cfg) {
				continue
			}
			if change.conflicted {
				return fmt.Errorf("%s: resolve the Git merge "+
					"conflict before formatting", path)
			}
			info, err := os.Lstat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				continue
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var baseline []byte
			if change.original != "" {
				baseline, err = gitOutput(
					root, "cat-file", "blob",
					change.original,
				)
				if err != nil {
					return err
				}
			}

			name, err := filepath.Rel(cwd, path)
			if err != nil {
				name = path
			}
			out, skipped, err := formatGitChanges(
				baseline, src, name, cfg,
			)
			if err != nil {
				return err
			}
			if skipped {
				fmt.Fprintf(
					stderr,
					"%s: skipped formatting that cannot "+
						"be completed within the "+
						"affected statements or "+
						"declarations\n",
					name,
				)
			}
			reported, err := reportChangedRuleDiagnostics(
				baseline, out, name, cfg, stderr,
			)
			if err != nil {
				return err
			}
			changed, err := outputFile(
				path, name, src, out, m, stdout,
			)
			if err != nil {
				return err
			}
			anyChanged = anyChanged || changed || skipped ||
				reported
		}
	}
	if m == modeCheck && anyChanged {
		return errChangesNeeded
	}
	return nil
}

func formatGitChanges(baseline, src []byte, name string,
	cfg *config.Config) ([]byte, bool, error) {

	hunks, err := gitLineEdits(baseline, src)
	if err != nil {
		return nil, false, err
	}
	changed := changedSpans(hunks)
	if len(changed) == 0 {
		return src, false, nil
	}
	if len(changed) == 1 && changed[0].start == 0 &&
		changed[0].end == len(sourceLines(src)) {

		out, _, err := format.Format(src, name, cfg)
		return out, false, err
	}
	scopes, err := formattingScopes(src, changed)
	if err != nil {
		return nil, false, err
	}
	if cfg.Rules.DeclarationGroupingOn() {
		var ranges []format.LineRange
		for _, span := range changed {
			ranges = append(ranges, format.LineRange{
				Start: span.start,
				End:   span.end,
			})
		}
		collected, err := format.CollectChangedDeclarations(
			src, name, ranges,
		)
		if err != nil {
			return nil, false, err
		}
		if !bytes.Equal(src, collected) {
			src = collected
			hunks, err = gitLineEdits(baseline, src)
			if err != nil {
				return nil, false, err
			}
			scopes, err = formattingScopes(src, changedSpans(hunks))
			if err != nil {
				return nil, false, err
			}
		}
	}
	return formatChangedLines(src, name, cfg, scopes)
}

func changedSpans(hunks []lineEdit) []lineSpan {
	var changed []lineSpan
	for _, hunk := range hunks {
		changed = append(changed, hunk.new)
	}
	return changed
}
