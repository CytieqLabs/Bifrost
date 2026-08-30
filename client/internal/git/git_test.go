package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotIncludesTrackedAndUntrackedWithoutChangingIndex(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-qm", "base")
	runGit(t, root, "add", "tracked.txt") // known clean index state
	indexBefore := runGit(t, root, "write-tree")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".bifrost"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".bifrost", "state.json"), []byte("secret metadata\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	parent, _ := repo.Head()
	checkpoint, err := repo.Snapshot(parent, "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	indexAfter := runGit(t, root, "write-tree")
	if indexBefore != indexAfter {
		t.Fatal("snapshot changed the user's index")
	}
	tree := runGit(t, root, "ls-tree", "-r", "--name-only", checkpoint)
	if !strings.Contains(tree, "tracked.txt") || !strings.Contains(tree, "new.txt") {
		t.Fatalf("checkpoint tree missing worktree files: %s", tree)
	}
	if strings.Contains(tree, ".bifrost") {
		t.Fatalf("checkpoint leaked Bifrost metadata: %s", tree)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
