package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/CytieqLabs/Bifrost/client/internal/model"
	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

var ErrNotInitialized = errors.New("Bifrost is not initialized; run `bifrost init`")

type Store struct {
	Root string
	Dir  string
}

func Open(root string) *Store { return &Store{Root: root, Dir: filepath.Join(root, ".bifrost")} }

func (s *Store) Initialize(repo model.Repository) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(s.statePath()); err == nil {
		return fmt.Errorf("Bifrost is already initialized")
	}
	return s.write(&model.State{Version: model.SchemaVersion, Repository: repo, Runs: []model.Run{}, Checkpoints: []model.Checkpoint{}, Evidence: []model.Evidence{}, Promotions: []model.Promotion{}, Events: []model.Event{}, Idempotency: map[string]model.IdempotencyReceipt{}})
}

func (s *Store) Load() (*model.State, error) {
	data, err := os.ReadFile(s.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotInitialized
	}
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("invalid Bifrost state: %w", err)
	}
	if envelope.Version == 1 {
		return migrateV1(data)
	}
	var state model.State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("invalid Bifrost state: %w", err)
	}
	if state.Version != model.SchemaVersion {
		return nil, fmt.Errorf("unsupported state version %d", state.Version)
	}
	if state.Events == nil {
		state.Events = []model.Event{}
	}
	if state.Idempotency == nil {
		state.Idempotency = map[string]model.IdempotencyReceipt{}
	}
	return &state, nil
}

type runV1 struct {
	ID           string    `json:"id"`
	Task         string    `json:"task"`
	Agent        string    `json:"agent"`
	ParentID     string    `json:"parent_id,omitempty"`
	BaseCommit   string    `json:"base_commit"`
	ResultCommit string    `json:"result_commit,omitempty"`
	Workspace    string    `json:"workspace,omitempty"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func migrateV1(data []byte) (*model.State, error) {
	var old struct {
		Version     int                `json:"version"`
		Repository  model.Repository   `json:"repository"`
		Runs        []runV1            `json:"runs"`
		Checkpoints []model.Checkpoint `json:"checkpoints"`
		Evidence    []model.Evidence   `json:"evidence"`
		Promotions  []model.Promotion  `json:"promotions"`
	}
	if err := json.Unmarshal(data, &old); err != nil {
		return nil, fmt.Errorf("invalid Bifrost v1 state: %w", err)
	}
	state := &model.State{
		Version: model.SchemaVersion, Repository: old.Repository,
		Checkpoints: old.Checkpoints, Evidence: old.Evidence, Promotions: old.Promotions,
		Runs: []model.Run{}, Events: []model.Event{}, Idempotency: map[string]model.IdempotencyReceipt{},
	}
	for _, run := range old.Runs {
		state.Runs = append(state.Runs, model.Run{
			ID: run.ID, Task: run.Task, Agent: run.Agent, ParentID: run.ParentID,
			BaseCommit: run.BaseCommit, ResultCommit: run.ResultCommit,
			Workspace: bifrostv1.Workspace{
				ID: "ws-legacy-" + run.ID, Mode: bifrostv1.WorkspaceLocal,
				State: bifrostv1.WorkspaceActive, Path: run.Workspace,
			},
			Status: bifrostv1.RunStatus(run.Status), CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt,
		})
	}
	return state, nil
}

func (s *Store) Update(fn func(*model.State) error) error {
	unlock, err := s.lock(3 * time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	state, err := s.Load()
	if err != nil {
		return err
	}
	if err := fn(state); err != nil {
		return err
	}
	return s.write(state)
}

func NewID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func (s *Store) statePath() string { return filepath.Join(s.Dir, "state.json") }

func (s *Store) write(state *model.State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, "state-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.statePath())
}

func (s *Store) lock(timeout time.Duration) (func(), error) {
	path := filepath.Join(s.Dir, "write.lock")
	deadline := time.Now().Add(timeout)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("Bifrost metadata is busy")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
