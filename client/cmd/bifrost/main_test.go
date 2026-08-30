package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CytieqLabs/Bifrost/client/internal/config"
	"github.com/CytieqLabs/Bifrost/client/internal/store"
	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

func TestRunLifecycle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BIFROST_CONFIG", filepath.Join(root, "client-config.json"))
	git(t, root, "init", "-q")
	git(t, root, "config", "user.name", "Test")
	git(t, root, "config", "user.email", "test@example.com")
	writeFile(t, filepath.Join(root, "app.txt"), "base\n")
	git(t, root, "add", "app.txt")
	git(t, root, "commit", "-qm", "base")
	initialHead := git(t, root, "rev-parse", "HEAD")

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	if err := run([]string{"init"}); err != nil {
		t.Fatal(err)
	}
	if ignored := git(t, root, "check-ignore", ".bifrost/state.json"); ignored != ".bifrost/state.json" {
		t.Fatalf("control state is not locally ignored: %q", ignored)
	}
	if err := run([]string{"run", "start", "--task", "implement feature", "--agent", "isha"}); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(root).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Runs) != 1 || state.Runs[0].Status != "running" {
		t.Fatalf("unexpected runs: %#v", state.Runs)
	}
	runID := state.Runs[0].ID
	workspace := filepath.Join(root, filepath.FromSlash(state.Runs[0].Workspace.Path))
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"status"}); err != nil {
		t.Fatalf("command from linked workspace could not find control state: %v", err)
	}

	writeFile(t, filepath.Join(workspace, "app.txt"), "implemented\n")
	writeFile(t, filepath.Join(workspace, "test-report.txt"), "all tests passed\n")
	if err := run([]string{"checkpoint", "create", "--run", runID, "--note", "feature complete"}); err != nil {
		t.Fatal(err)
	}
	if head := git(t, root, "rev-parse", "HEAD"); head != initialHead {
		t.Fatalf("checkpoint moved the primary branch: got %s want %s", head, initialHead)
	}
	if refs := git(t, root, "for-each-ref", "--format=%(refname)", "refs/bifrost"); refs != "" {
		t.Fatalf("checkpoint created internal refs: %s", refs)
	}
	if err := os.Symlink(filepath.Join(root, "app.txt"), filepath.Join(workspace, "outside-link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"evidence", "add", "--run", runID, "--kind", "test", "--artifact", "outside-link.txt"}); err == nil {
		t.Fatal("evidence accepted a symlink escaping the run workspace")
	}
	if err := run([]string{"evidence", "add", "--run", runID, "--kind", "test", "--command", "go test ./...", "--summary", "passed", "--artifact", "test-report.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"run", "finish", runID}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"promotion", "check", "--run", runID}); err != nil {
		t.Fatal(err)
	}

	state, err = store.Open(root).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Checkpoints) != 1 || len(state.Evidence) != 1 {
		t.Fatalf("missing lifecycle records: %#v", state)
	}
	if state.Runs[0].Status != "completed" || state.Runs[0].ResultCommit != state.Checkpoints[0].Commit {
		t.Fatalf("run did not finish at checkpoint: %#v", state.Runs[0])
	}
	if state.Evidence[0].ArtifactSHA256 == "" {
		t.Fatal("evidence artifact was not hashed")
	}
	if len(state.Promotions) != 1 || state.Promotions[0].Status != "ready" {
		t.Fatalf("expected a ready promotion: %#v", state.Promotions)
	}
	tree := git(t, root, "ls-tree", "-r", "--name-only", state.Checkpoints[0].Commit)
	if strings.Contains(tree, ".bifrost") {
		t.Fatalf("checkpoint leaked control state: %s", tree)
	}

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "app.txt"), "competing change\n")
	git(t, root, "add", "app.txt")
	git(t, root, "commit", "-qm", "competing change")
	if err := run([]string{"promotion", "apply", state.Promotions[0].ID}); err == nil {
		t.Fatal("promotion applied after its reviewed target advanced")
	}
	if err := run([]string{"promotion", "check", "--run", runID}); err != nil {
		t.Fatal(err)
	}
	state, err = store.Open(root).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Promotions) != 2 || state.Promotions[0].Status != "stale" || state.Promotions[1].Status != "conflicted" || len(state.Promotions[1].Conflicts) == 0 {
		t.Fatalf("expected conflict-aware promotion: %#v", state.Promotions)
	}
	if err := run([]string{"workspace", "remove", runID, "--force"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("workspace still exists after removal: %v", err)
	}
	state, err = store.Open(root).Load()
	if err != nil || state.Runs[0].Workspace.State != bifrostv1.WorkspaceRemoved {
		t.Fatalf("workspace removal was not recorded: %#v, %v", state.Runs[0].Workspace, err)
	}
}

func TestProfileLifecycleDoesNotRequireGitRepository(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("BIFROST_CONFIG", path)
	if err := run([]string{"profile", "add", "cloud", "--mode", "remote", "--endpoint", "https://bifrost.example", "--token-env", "BIFROST_TOKEN", "--use"}); err != nil {
		t.Fatal(err)
	}
	settings, err := (&config.Store{Path: path}).Load()
	if err != nil || settings.Current != "cloud" || settings.Profiles["cloud"].Mode != config.ModeRemote {
		t.Fatalf("profile was not saved: %#v, %v", settings, err)
	}
	if err := run([]string{"profile", "add", "cloud", "--mode", "remote", "--endpoint", "https://other.example"}); err == nil {
		t.Fatal("existing profile was overwritten without --replace")
	}
	if err := run([]string{"profile", "use", "local"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"profile", "remove", "cloud"}); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalHelpDoesNotRequireRepository(t *testing.T) {
	if err := run([]string{"--help"}); err != nil {
		t.Fatal(err)
	}
}

func TestGitPassthroughAndCommitAlias(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q")
	git(t, root, "config", "user.name", "Test")
	git(t, root, "config", "user.email", "test@example.com")
	writeFile(t, filepath.Join(root, "README.md"), "hello\n")
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := run([]string{"git", "add", "README.md"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"commit", "-m", "initial"}); err != nil {
		t.Fatal(err)
	}
	if got := git(t, root, "log", "-1", "--pretty=%s"); got != "initial" {
		t.Fatalf("commit alias did not create commit: %q", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
