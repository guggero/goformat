package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type testRepo struct {
	t    *testing.T
	root string
}

func newTestRepo(t *testing.T) testRepo {
	t.Helper()
	r := testRepo{t, t.TempDir()}
	r.git("init", "-q")
	r.git("config", "user.name", "Formatter Test")
	r.git("config", "user.email", "formatter@example.invalid")
	r.git("config", "core.hooksPath", "/dev/null")
	r.git("config", "core.autocrlf", "false")
	return r
}

func (r testRepo) git(args ...string) []byte {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.root}, args...)...)
	cmd.Env = append(
		os.Environ(), "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func (r testRepo) write(path, contents string) string {
	r.t.Helper()
	path = filepath.Join(r.root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		r.t.Fatal(err)
	}
	return path
}

func (r testRepo) read(path string) string {
	r.t.Helper()
	contents, err := os.ReadFile(filepath.Join(r.root, path))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(contents)
}

func (r testRepo) commit() {
	r.t.Helper()
	r.git("add", ".")
	r.git("-c", "commit.gpgsign=false", "commit", "-qm", "baseline")
}

func runForTest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, diagnostics bytes.Buffer
	err := run(
		append([]string{
			"--no-config",
		}, args...), strings.NewReader(""),
		&out, &diagnostics,
	)
	return out.String(), diagnostics.String(), err
}

func TestGitSelectionAndIndexPreservation(t *testing.T) {
	for _, selection := range []string{
		"--unstaged-only", "--uncommitted-" +
			"only",
	} {

		t.Run(selection, func(t *testing.T) {
			r := newTestRepo(t)
			baseline := `package p

func untouched( ){println( 0 )}

func staged() {
	println(1)
}

func unstaged() {
	println(2)
}
`
			path := r.write("x.go", baseline)
			r.commit()
			staged := strings.Replace(
				baseline, "println(1)", "println( 3,4 )", 1,
			)
			r.write("x.go", staged)
			r.git("add", "x.go")
			working := strings.Replace(
				staged, "println(2)", "println( 5,6 )", 1,
			)
			r.write("x.go", working)
			_, diagnostics, err := runForTest(
				t, selection, "-w", path,
			)
			if err != nil || diagnostics != "" {
				t.Fatalf("format: %v %s", err, diagnostics)
			}
			want := strings.Replace(
				working, "println( 5,6 )", "println(5, 6)", 1,
			)
			if selection == "--uncommitted-only" {
				want = strings.Replace(
					want, "println( 3,4 )", "println(3, 4)",
					1,
				)
			}
			if got := r.read("x.go"); got != want {
				t.Fatalf("wrong selection:\n%s\nwant:\n%s", got, want)
			}
			if !bytes.Equal(r.git("show", ":x.go"), []byte(
				staged,
			)) {

				t.Fatal("index was modified")
			}
			if !bytes.Equal(r.git("show", "HEAD:x.go"), []byte(
				baseline,
			)) {

				t.Fatal("commit was modified")
			}
			out, _, err := runForTest(t, selection, "-l", path)
			if err != nil || out != "" {
				t.Fatalf("not idempotent: %v %s", err, out)
			}
		})
	}
}

func TestGitPathsExcludesAndUntracked(t *testing.T) {
	r := newTestRepo(t)
	r.write("tracked.go", "package p\n")
	r.write(".gitignore", "ignored.go\n")
	r.commit()
	bad := "package p\n\nfunc f( ){println( 1 )}\n"
	r.write("a/new.go", bad)
	r.write("b/new.go", bad)
	r.write("a/skip.go", bad)
	r.write("a/vendor/v.go", bad)
	r.write("ignored.go", bad)
	_, diagnostics, err := runForTest(
		t, "--unstaged-only", "-w", "-exclude", "skip.go",
		filepath.Join(r.root, "a"),
	)
	if err != nil || diagnostics != "" {
		t.Fatalf("format: %v %s", err, diagnostics)
	}
	if r.read("a/new.go") == bad {
		t.Fatal("untracked file not formatted")
	}
	for _, path := range []string{
		"b/new.go", "a/skip.go", "a/vendor/v.go", "ignored.go",
	} {

		if r.read(path) != bad {
			t.Fatalf("excluded file changed: %s", path)
		}
	}
}

func TestGitModesAndErrors(t *testing.T) {
	r := newTestRepo(t)
	r.write("base.go", "package p\n")
	r.commit()
	bad := "package p\n\nfunc f( ){println( 1 )}\n"
	path := r.write("new.go", bad)
	for _, m := range []string{"-d", "-l", "-check"} {
		out, _, err := runForTest(t, "--uncommitted-only", m, path)
		if m == "-check" {
			if !errors.Is(err, errChangesNeeded) {
				t.Fatalf("check: %v", err)
			}
		} else if err != nil || out == "" {
			t.Fatalf("mode %s: %v %q", m, err, out)
		}
		if r.read("new.go") != bad {
			t.Fatalf("read-only mode %s wrote output", m)
		}
	}
	for _, args := range [][]string{
		{"--uncommitted-only", "--unstaged-only", "-w", path},
		{"--unstaged-only", "-"},
		{"--unstaged-only", t.TempDir()},
	} {

		if _, _, err := runForTest(t, args...); err == nil {
			t.Fatalf("expected error for %v", args)
		}
	}
}

func TestGitUnbornAndStagedOnly(t *testing.T) {
	r := newTestRepo(t)
	bad := "package p\n\nfunc f( ){println( 1 )}\n"
	r.write("staged.go", bad)
	r.git("add", "staged.go")
	if _, _, err := runForTest(
		t, "--unstaged-only", "-w", r.root,
	); err != nil {

		t.Fatal(err)
	}
	if r.read("staged.go") != bad {
		t.Fatal("staged-only file formatted by --unstaged-only")
	}
	if _, _, err := runForTest(
		t, "--uncommitted-only", "-w", r.root,
	); err != nil {

		t.Fatal(err)
	}
	if r.read("staged.go") == bad {
		t.Fatal("unborn repository addition not formatted")
	}
	if !bytes.Equal(r.git("show", ":staged.go"), []byte(bad)) {
		t.Fatal("unborn index changed")
	}
}

func TestGitRenameDoesNotSelectWholeFile(t *testing.T) {
	r := newTestRepo(t)
	baseline := `package p

func untouched( ){println( 0 )}

func changed() {
	println(1)
}
`
	r.write("old.go", baseline)
	r.commit()
	name := "renamed with spaces\nand tab\t.go"
	r.git("mv", "old.go", name)
	if _, _, err := runForTest(
		t, "--uncommitted-only", "-w", r.root,
	); err != nil {

		t.Fatal(err)
	}
	if r.read(name) != baseline {
		t.Fatal("rename selected committed contents")
	}
	r.write(name, strings.Replace(
		baseline, "println(1)", "println( 2,3 )", 1,
	))
	if _, _, err := runForTest(
		t, "--uncommitted-only", "-w", r.root,
	); err != nil {

		t.Fatal(err)
	}
	want := strings.Replace(baseline, "println(1)", "println(2, 3)", 1)
	if r.read(name) != want {
		t.Fatalf("rename change formatted unrelated code:\n%s", r.read(
			name,
		))
	}
}

func TestGitDefaultScopeAndOverlappingPaths(t *testing.T) {
	r := newTestRepo(t)
	r.write("base.go", "package p\n")
	r.commit()
	bad := "package p\n\nfunc f( ){println( 1 )}\n"
	r.write("a/x.go", bad)
	r.write("b/y.go", bad)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join(r.root, "a")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
	})
	if _, _, err := runForTest(t, "--unstaged-only", "-w"); err != nil {
		t.Fatal(err)
	}
	if r.read("a/x.go") == bad || r.read("b/y.go") != bad {
		t.Fatal("default scope escaped the current directory")
	}
	r.write("a/x.go", bad)
	out, _, err := runForTest(t, "--unstaged-only", "-l", ".", "x.go")
	if err != nil || strings.Count(out, "x.go") != 1 {
		t.Fatalf("overlapping scopes duplicated a file: %v %q", err, out)
	}
}

func TestGitDeletedFileAndSymlink(t *testing.T) {
	r := newTestRepo(t)
	path := r.write("deleted.go", "package p\n")
	r.commit()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "target.go")
	bad := []byte("package p\nfunc f( ){}\n")
	if err := os.WriteFile(outside, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(
		r.root, "link.go",
	)); err != nil {

		t.Fatal(err)
	}
	if _, _, err := runForTest(
		t, "--uncommitted-only", "-w", r.root,
	); err != nil {

		t.Fatal(err)
	}
	got, err := os.ReadFile(outside)
	if err != nil || !bytes.Equal(got, bad) {
		t.Fatal("symlink target changed")
	}
}
