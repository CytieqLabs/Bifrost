package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Repository struct{ Root string }

// Run executes a regular Git command in the repository's primary worktree.
// Bifrost intentionally delegates storage and transport semantics to Git.
func (r *Repository) Run(args ...string) error {
	if len(args) == 0 {
		return errors.New("git command is required")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Root
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

type MergeResult struct {
	Tree      string
	Conflicts []string
}

func Discover(dir string) (*Repository, error) {
	out, err := command(dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a Git repository: %w", err)
	}
	return &Repository{Root: strings.TrimSpace(out)}, nil
}

func (r *Repository) Head() (string, error) {
	out, err := command(r.Root, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("repository needs an initial commit: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (r *Repository) Remote() string {
	out, _ := command(r.Root, nil, "remote", "get-url", "origin")
	return strings.TrimSpace(out)
}

// ControlRoot returns the primary worktree. Linked agent worktrees use this
// location to find the single shared Bifrost state store.
func (r *Repository) ControlRoot() (string, error) {
	out, err := command(r.Root, nil, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			return strings.TrimPrefix(line, "worktree "), nil
		}
	}
	return "", errors.New("Git did not report a primary worktree")
}

func (r *Repository) Resolve(revision string) (string, error) {
	out, err := command(r.Root, nil, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", revision, err)
	}
	return strings.TrimSpace(out), nil
}

func (r *Repository) BranchRef(revision string) (string, error) {
	out, err := command(r.Root, nil, "rev-parse", "--symbolic-full-name", revision)
	if err != nil {
		return "", fmt.Errorf("resolve branch %q: %w", revision, err)
	}
	ref := strings.TrimSpace(out)
	if !strings.HasPrefix(ref, "refs/heads/") {
		return "", fmt.Errorf("promotion target %q is not a local branch", revision)
	}
	return ref, nil
}

func (r *Repository) CheckedOutWorktree(ref string) (string, bool, error) {
	out, err := command(r.Root, nil, "worktree", "list", "--porcelain")
	if err != nil {
		return "", false, err
	}
	worktree := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			worktree = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch ") && strings.TrimPrefix(line, "branch ") == ref:
			return worktree, true, nil
		}
	}
	return "", false, nil
}

func (r *Repository) IsAncestor(ancestor, descendant string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", ancestor, descendant)
	cmd.Dir = r.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor: %s", strings.TrimSpace(stderr.String()))
}

func (r *Repository) CreateWorktree(path, commit string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_, err := command(r.Root, nil, "worktree", "add", "--detach", path, commit)
	return err
}

func (r *Repository) RemoveWorktree(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := command(r.Root, nil, args...)
	return err
}

// Merge computes a virtual merge tree without changing refs, indexes, or
// worktrees. A non-empty conflict list means the result is not promotable.
func (r *Repository) Merge(target, result string) (MergeResult, error) {
	cmd := exec.Command("git", "merge-tree", "--write-tree", "--name-only", target, result)
	cmd.Dir = r.Root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	lines := nonEmptyLines(stdout.String())
	merged := MergeResult{}
	if len(lines) > 0 && len(lines[0]) == 40 {
		merged.Tree = lines[0]
		lines = lines[1:]
	}
	if err == nil {
		return merged, nil
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		for _, line := range lines {
			if !strings.Contains(line, "CONFLICT") && !strings.HasPrefix(line, "Auto-merging ") {
				merged.Conflicts = append(merged.Conflicts, line)
			}
		}
		if len(merged.Conflicts) == 0 {
			merged.Conflicts = []string{"merge conflict"}
		}
		return merged, nil
	}
	return MergeResult{}, fmt.Errorf("git merge-tree: %s", strings.TrimSpace(stderr.String()))
}

func (r *Repository) CommitTree(tree string, parents []string, message string) (string, error) {
	args := []string{"commit-tree", tree}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	env := []string{
		"GIT_AUTHOR_NAME=Bifrost", "GIT_AUTHOR_EMAIL=bifrost@local",
		"GIT_COMMITTER_NAME=Bifrost", "GIT_COMMITTER_EMAIL=bifrost@local",
	}
	out, err := commandInput(r.Root, env, message+"\n", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// UpdateRefCAS moves a local ref only when it still points at oldCommit.
func (r *Repository) UpdateRefCAS(ref, newCommit, oldCommit, reason string) error {
	_, err := command(r.Root, nil, "update-ref", "-m", reason, ref, newCommit, oldCommit)
	return err
}

func (r *Repository) Dirty() (bool, error) {
	out, err := command(r.Root, nil, "status", "--porcelain=v1", "--untracked-files=all")
	return strings.TrimSpace(out) != "", err
}

// ExcludeControlState adds Bifrost's private state directory to Git's local
// exclude file. This keeps project-owned .gitignore files untouched.
func (r *Repository) ExcludeControlState() error {
	out, err := command(r.Root, nil, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	path := strings.TrimSpace(out)
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.Root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "/.bifrost/" {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	prefix := ""
	if len(data) > 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "%s# Bifrost local control state\n/.bifrost/\n", prefix)
	return err
}

// Snapshot writes the worktree, including untracked files, into Git's object
// database using an isolated temporary index. It never changes the user's
// index, branch, worktree, or refs.
func (r *Repository) Snapshot(parent, message string) (string, error) {
	index, err := os.CreateTemp("", "bifrost-index-*")
	if err != nil {
		return "", err
	}
	path := index.Name()
	index.Close()
	_ = os.Remove(path) // Git expects a missing or valid index, not an empty file.
	defer os.Remove(path)
	env := []string{
		"GIT_INDEX_FILE=" + path,
		"GIT_AUTHOR_NAME=Bifrost", "GIT_AUTHOR_EMAIL=bifrost@local",
		"GIT_COMMITTER_NAME=Bifrost", "GIT_COMMITTER_EMAIL=bifrost@local",
	}
	if _, err = command(r.Root, env, "read-tree", parent); err != nil {
		return "", err
	}
	if _, err = command(r.Root, env, "add", "-A"); err != nil {
		return "", err
	}
	// Defense in depth: remove control-plane state from the isolated index even
	// if a snapshot is invoked before the repository's local exclude is set.
	if _, err = command(r.Root, env, "rm", "--cached", "-r", "--ignore-unmatch", "--", ".bifrost"); err != nil {
		return "", err
	}
	tree, err := command(r.Root, env, "write-tree")
	if err != nil {
		return "", err
	}
	commit, err := commandInput(r.Root, env, message+"\n", "commit-tree", strings.TrimSpace(tree), "-p", parent)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(commit), nil
}

func command(dir string, extraEnv []string, args ...string) (string, error) {
	return commandInput(dir, extraEnv, "", args...)
}

func commandInput(dir string, extraEnv []string, input string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
