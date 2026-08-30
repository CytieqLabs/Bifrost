package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CytieqLabs/Bifrost/client/internal/model"
)

func TestStoreInitializeAndUpdate(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if err := s.Initialize(model.Repository{ID: "repo-1", Root: root, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(state *model.State) error {
		state.Runs = append(state.Runs, model.Run{ID: "run-1", Status: "running"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	state, err := s.Load()
	if err != nil || state.Run("run-1") == nil {
		t.Fatalf("updated state was not durable: %#v, %v", state, err)
	}
	if filepath.Base(s.statePath()) != "state.json" {
		t.Fatal("unexpected state path")
	}
}

func TestLoadRequiresInitialization(t *testing.T) {
	if _, err := Open(t.TempDir()).Load(); err != ErrNotInitialized {
		t.Fatalf("expected ErrNotInitialized, got %v", err)
	}
}

func TestLoadMigratesV1WorkspacePath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".bifrost")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{
  "version": 1,
  "repository": {"id":"repo-1","root":"/repo","created_at":"2026-01-01T00:00:00Z"},
  "runs": [{"id":"run-1","task":"work","agent":"isha","base_commit":"abc","workspace":".bifrost/workspaces/run-1","status":"running","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}],
  "checkpoints": [], "evidence": [], "promotions": []
}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := Open(root).Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != model.SchemaVersion || state.Runs[0].Workspace.Path != ".bifrost/workspaces/run-1" || state.Runs[0].Workspace.ID != "ws-legacy-run-1" {
		t.Fatalf("unexpected migration: %#v", state)
	}
	if state.Events == nil || state.Idempotency == nil {
		t.Fatal("migration must initialize reliability state")
	}
}
