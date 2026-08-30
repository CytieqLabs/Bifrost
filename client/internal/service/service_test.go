package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CytieqLabs/Bifrost/client/internal/model"
)

func TestApplyPromotionFastForwardsReviewedBranch(t *testing.T) {
	root := initializedRepo(t)
	app, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Initialize(); err != nil {
		t.Fatal(err)
	}
	git(t, root, "branch", "integration")
	git(t, root, "branch", "recovery")
	run, err := app.StartRun("implement feature", "isha", "")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(app.WorkspacePath(*run), "app.txt"), "implemented\n")
	checkpoint, err := app.CreateCheckpoint(run.ID, "done")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.AddEvidence(run.ID, EvidenceInput{Kind: "test", Command: "go test ./...", Summary: "passed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FinishRun(run.ID, "completed", ""); err != nil {
		t.Fatal(err)
	}
	checkedOut, err := app.CheckPromotion(run.ID, "HEAD")
	if err != nil || checkedOut.Status != "ready" {
		t.Fatalf("checked-out promotion was not ready for review: %#v, %v", checkedOut, err)
	}
	if _, err := app.ApplyPromotion(checkedOut.ID); err == nil || !strings.Contains(err.Error(), "checked out") {
		t.Fatalf("checked-out branch should be protected, got %v", err)
	}
	write(t, filepath.Join(root, "release.txt"), "target-side change\n")
	git(t, root, "add", "release.txt")
	git(t, root, "commit", "-qm", "advance target")
	git(t, root, "branch", "-f", "integration", "HEAD")
	promotion, err := app.CheckPromotion(run.ID, "integration")
	if err != nil || promotion.Status != "ready" {
		t.Fatalf("promotion was not ready: %#v, %v", promotion, err)
	}
	applied, err := app.ApplyPromotion(promotion.ID)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Status != "applied" || applied.AppliedAt == nil || applied.AppliedCommit == "" || applied.AppliedCommit == checkpoint.Commit {
		t.Fatalf("unexpected apply receipt: %#v", applied)
	}
	if head := git(t, root, "rev-parse", "refs/heads/integration"); head != applied.AppliedCommit {
		t.Fatalf("integration branch was not atomically advanced: got %s want %s", head, applied.AppliedCommit)
	}
	parents := strings.Fields(git(t, root, "rev-list", "--parents", "-n", "1", applied.AppliedCommit))
	if len(parents) != 3 || parents[1] != promotion.TargetCommit || parents[2] != checkpoint.Commit {
		t.Fatalf("promotion merge has wrong parents: %v", parents)
	}
	if head := git(t, root, "rev-parse", "HEAD"); head != promotion.TargetCommit {
		t.Fatal("promotion unexpectedly moved the checked-out branch")
	}

	recovery, err := app.CheckPromotion(run.ID, "recovery")
	if err != nil || recovery.Status != "ready" {
		t.Fatalf("recovery promotion was not ready: %#v, %v", recovery, err)
	}
	if err := app.Store.Update(func(state *model.State) error {
		promotion := state.Promotion(recovery.ID)
		promotion.Status, promotion.AppliedCommit = "applying", checkpoint.Commit
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	recovered, err := app.ApplyPromotion(recovery.ID)
	if err != nil || recovered.Status != "applied" {
		t.Fatalf("interrupted promotion did not recover: %#v, %v", recovered, err)
	}
	if head := git(t, root, "rev-parse", "refs/heads/recovery"); head != checkpoint.Commit {
		t.Fatalf("recovered branch was not advanced: got %s want %s", head, checkpoint.Commit)
	}
}

func initializedRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q")
	git(t, root, "config", "user.name", "Test")
	git(t, root, "config", "user.email", "test@example.com")
	write(t, filepath.Join(root, "app.txt"), "base\n")
	git(t, root, "add", "app.txt")
	git(t, root, "commit", "-qm", "base")
	return root
}

func write(t *testing.T, path, content string) {
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
