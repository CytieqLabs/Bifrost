package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gitrepo "github.com/CytieqLabs/Bifrost/client/internal/git"
	"github.com/CytieqLabs/Bifrost/client/internal/model"
	"github.com/CytieqLabs/Bifrost/client/internal/store"
	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

type Service struct {
	Repo  *gitrepo.Repository
	Store *store.Store
}

type Status = bifrostv1.Status
type RunDetails = bifrostv1.RunDetails
type EvidenceInput = bifrostv1.AddEvidenceRequest

func Open(dir string) (*Service, error) {
	current, err := gitrepo.Discover(dir)
	if err != nil {
		return nil, err
	}
	root, err := current.ControlRoot()
	if err != nil {
		return nil, err
	}
	repo, err := gitrepo.Discover(root)
	if err != nil {
		return nil, err
	}
	return &Service{Repo: repo, Store: store.Open(root)}, nil
}

func (s *Service) Initialize() (*model.Repository, error) {
	head, err := s.Repo.Head()
	if err != nil {
		return nil, err
	}
	if err := s.Repo.ExcludeControlState(); err != nil {
		return nil, fmt.Errorf("configure local Git exclude: %w", err)
	}
	repository := model.Repository{ID: store.NewID("repo"), Root: s.Repo.Root, Remote: s.Repo.Remote(), CreatedAt: time.Now().UTC()}
	if err := s.Store.Initialize(repository); err != nil {
		return nil, err
	}
	_ = head // Resolving HEAD above enforces the initial-commit requirement.
	return &repository, nil
}

func (s *Service) Status() (Status, error) {
	state, err := s.Store.Load()
	if err != nil {
		return Status{}, err
	}
	head, err := s.Repo.Head()
	if err != nil {
		return Status{}, err
	}
	dirty, err := s.Repo.Dirty()
	if err != nil {
		return Status{}, err
	}
	result := Status{Repository: filepath.Base(s.Repo.Root), Head: head, Dirty: dirty, Runs: len(state.Runs), Checkpoints: len(state.Checkpoints), Evidence: len(state.Evidence), Promotions: len(state.Promotions)}
	for _, run := range state.Runs {
		if run.Status == "running" {
			result.ActiveRuns++
		}
	}
	return result, nil
}

func (s *Service) StartRun(task, agent, parentID string) (*model.Run, error) {
	task, agent = strings.TrimSpace(task), strings.TrimSpace(agent)
	if task == "" {
		return nil, errors.New("task is required")
	}
	if agent == "" {
		return nil, errors.New("agent is required")
	}
	base, err := s.Repo.Head()
	if err != nil {
		return nil, err
	}
	state, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	if parentID != "" {
		parent := state.Run(parentID)
		if parent == nil {
			return nil, fmt.Errorf("parent run %q not found", parentID)
		}
		base = latestRevision(state, parent)
	}
	now := time.Now().UTC()
	run := model.Run{ID: store.NewID("run"), Task: task, Agent: agent, ParentID: parentID, BaseCommit: base, Status: "running", CreatedAt: now, UpdatedAt: now}
	run.Workspace = bifrostv1.Workspace{
		ID: store.NewID("ws"), Mode: bifrostv1.WorkspaceLocal,
		State: bifrostv1.WorkspaceActive,
		Path:  filepath.ToSlash(filepath.Join(".bifrost", "workspaces", run.ID)),
	}
	workspace := s.WorkspacePath(run)
	if err := s.Repo.CreateWorktree(workspace, run.BaseCommit); err != nil {
		return nil, fmt.Errorf("create run workspace: %w", err)
	}
	if err := s.Store.Update(func(current *model.State) error {
		if parentID != "" && current.Run(parentID) == nil {
			return fmt.Errorf("parent run %q disappeared", parentID)
		}
		current.Runs = append(current.Runs, run)
		return nil
	}); err != nil {
		_ = s.Repo.RemoveWorktree(workspace, true)
		return nil, err
	}
	s.recordEvent(bifrostv1.EventRunCreated, run.ID, run)
	return &run, nil
}

func (s *Service) Runs() ([]model.Run, error) {
	state, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	runs := append([]model.Run(nil), state.Runs...)
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt.After(runs[j].CreatedAt) })
	return runs, nil
}

func (s *Service) Run(id string) (*RunDetails, error) {
	state, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	run := state.Run(id)
	if run == nil {
		return nil, fmt.Errorf("run %q not found", id)
	}
	return &RunDetails{Run: *run, Checkpoints: checkpointsFor(state, id), Evidence: evidenceFor(state, id), Promotions: promotionsFor(state, id)}, nil
}

func (s *Service) FinishRun(id, status, result string) (*model.Run, error) {
	if status == "" {
		status = "completed"
	}
	if status != "completed" && status != "failed" && status != "cancelled" {
		return nil, errors.New("status must be completed, failed, or cancelled")
	}
	if result != "" {
		resolved, err := s.Repo.Resolve(result)
		if err != nil {
			return nil, err
		}
		result = resolved
	}
	var finished model.Run
	err := s.Store.Update(func(state *model.State) error {
		run := state.Run(id)
		if run == nil {
			return fmt.Errorf("run %q not found", id)
		}
		if run.Status != "running" {
			return fmt.Errorf("run is already %s", run.Status)
		}
		if result != "" {
			ancestor, err := s.Repo.IsAncestor(run.BaseCommit, result)
			if err != nil {
				return err
			}
			if !ancestor {
				return errors.New("result commit does not descend from the run base")
			}
		}
		run.Status, run.ResultCommit, run.UpdatedAt = bifrostv1.RunStatus(status), result, time.Now().UTC()
		if run.ResultCommit == "" {
			run.ResultCommit = latestCheckpoint(state, id)
		}
		finished = *run
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.recordEvent(bifrostv1.EventRunUpdated, finished.ID, finished)
	return &finished, nil
}

func (s *Service) CreateCheckpoint(runID, note string) (*model.Checkpoint, error) {
	state, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	run := state.Run(runID)
	if run == nil || run.Status != "running" {
		return nil, fmt.Errorf("active run %q not found", runID)
	}
	parent := latestRevision(state, run)
	workspaceRepo, err := gitrepo.Discover(s.WorkspacePath(*run))
	if err != nil {
		return nil, fmt.Errorf("run workspace: %w", err)
	}
	commit, err := workspaceRepo.Snapshot(parent, "bifrost checkpoint "+run.ID+"\n\n"+strings.TrimSpace(note))
	if err != nil {
		return nil, err
	}
	checkpoint := model.Checkpoint{ID: store.NewID("cp"), RunID: run.ID, Commit: commit, Note: strings.TrimSpace(note), CreatedAt: time.Now().UTC()}
	if err := s.Store.Update(func(current *model.State) error {
		live := current.Run(run.ID)
		if live == nil || live.Status != "running" {
			return errors.New("run stopped before checkpoint was recorded")
		}
		if latestRevision(current, live) != parent {
			return errors.New("run checkpoint advanced concurrently; retry the checkpoint")
		}
		current.Checkpoints = append(current.Checkpoints, checkpoint)
		return nil
	}); err != nil {
		return nil, err
	}
	s.recordEvent(bifrostv1.EventCheckpointCreated, run.ID, checkpoint)
	return &checkpoint, nil
}

func (s *Service) AddEvidence(runID string, input EvidenceInput) (*model.Evidence, error) {
	valid := map[bifrostv1.EvidenceKind]bool{
		bifrostv1.EvidenceTest: true, bifrostv1.EvidenceBuild: true,
		bifrostv1.EvidenceScan: true, bifrostv1.EvidenceReview: true,
	}
	if runID == "" || !valid[input.Kind] {
		return nil, errors.New("run and kind test|build|scan|review are required")
	}
	state, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	run := state.Run(runID)
	if run == nil {
		return nil, fmt.Errorf("run %q not found", runID)
	}
	record := model.Evidence{ID: store.NewID("ev"), RunID: runID, Kind: input.Kind, Command: input.Command, ExitCode: input.ExitCode, Summary: input.Summary, CreatedAt: time.Now().UTC()}
	if input.Artifact != "" {
		artifact := input.Artifact
		if !filepath.IsAbs(artifact) {
			artifact = filepath.Join(s.WorkspacePath(*run), artifact)
		}
		absolute, err := filepath.EvalSymlinks(artifact)
		if err != nil {
			return nil, fmt.Errorf("artifact: %w", err)
		}
		workspace, err := filepath.EvalSymlinks(s.WorkspacePath(*run))
		if err != nil {
			return nil, fmt.Errorf("run workspace: %w", err)
		}
		relative, err := filepath.Rel(workspace, absolute)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, errors.New("artifact must be inside the run workspace")
		}
		data, err := os.ReadFile(absolute)
		if err != nil {
			return nil, fmt.Errorf("artifact: %w", err)
		}
		digest := sha256.Sum256(data)
		record.Artifact, record.ArtifactSHA256 = filepath.ToSlash(relative), hex.EncodeToString(digest[:])
	}
	if err := s.Store.Update(func(current *model.State) error {
		if current.Run(runID) == nil {
			return fmt.Errorf("run %q disappeared", runID)
		}
		current.Evidence = append(current.Evidence, record)
		return nil
	}); err != nil {
		return nil, err
	}
	s.recordEvent(bifrostv1.EventEvidenceCreated, runID, record)
	return &record, nil
}

func (s *Service) CheckPromotion(runID, target string) (*model.Promotion, error) {
	if target == "" {
		target = "HEAD"
	}
	state, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	run := state.Run(runID)
	if run == nil {
		return nil, fmt.Errorf("run %q not found", runID)
	}
	targetCommit, err := s.Repo.Resolve(target)
	if err != nil {
		return nil, err
	}
	targetRef := target
	if branch, err := s.Repo.BranchRef(target); err == nil {
		targetRef = branch
	} else {
		// A commit can still be checked for conflicts, but this local service only
		// applies promotions to compare-and-swap protected local branches.
		targetRef = target
	}
	promotion := model.Promotion{ID: store.NewID("prom"), RunID: run.ID, TargetRef: targetRef, TargetCommit: targetCommit, ResultCommit: run.ResultCommit, Status: "ready", CreatedAt: time.Now().UTC()}
	if run.Status != "completed" {
		promotion.PolicyFailures = append(promotion.PolicyFailures, "run must be completed")
	}
	if run.ResultCommit == "" {
		promotion.PolicyFailures = append(promotion.PolicyFailures, "run has no result checkpoint")
	}
	if !hasPassingTest(state, run.ID) {
		promotion.PolicyFailures = append(promotion.PolicyFailures, "passing test evidence is required")
	}
	if _, err := s.Repo.BranchRef(target); err != nil {
		promotion.PolicyFailures = append(promotion.PolicyFailures, "target must resolve to a local branch")
	}
	if run.ResultCommit != "" {
		ancestor, err := s.Repo.IsAncestor(run.BaseCommit, targetCommit)
		if err != nil {
			return nil, err
		}
		if !ancestor {
			promotion.Status = "stale"
			promotion.PolicyFailures = append(promotion.PolicyFailures, "target no longer descends from run base")
		} else {
			merge, err := s.Repo.Merge(targetCommit, run.ResultCommit)
			if err != nil {
				return nil, err
			}
			promotion.MergeTree, promotion.Conflicts = merge.Tree, merge.Conflicts
			if len(merge.Conflicts) > 0 {
				promotion.Status = "conflicted"
			}
		}
	}
	if promotion.Status == "ready" && len(promotion.PolicyFailures) > 0 {
		promotion.Status = "blocked"
	}
	if err := s.Store.Update(func(current *model.State) error {
		if current.Run(run.ID) == nil {
			return fmt.Errorf("run %q disappeared", run.ID)
		}
		current.Promotions = append(current.Promotions, promotion)
		return nil
	}); err != nil {
		return nil, err
	}
	s.recordEvent(bifrostv1.EventPromotionChecked, run.ID, promotion)
	return &promotion, nil
}

func (s *Service) ApplyPromotion(id string) (*model.Promotion, error) {
	state, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	promotion := state.Promotion(id)
	if promotion == nil {
		return nil, fmt.Errorf("promotion %q not found", id)
	}
	if promotion.Status == "applying" {
		return s.resumePromotion(*promotion)
	}
	if promotion.Status != "ready" {
		return nil, fmt.Errorf("promotion is %s, not ready", promotion.Status)
	}
	branch, err := s.Repo.BranchRef(promotion.TargetRef)
	if err != nil {
		return nil, err
	}
	current, err := s.Repo.Resolve(branch)
	if err != nil {
		return nil, err
	}
	if current != promotion.TargetCommit {
		_ = s.markPromotionStale(id, "target branch advanced after promotion check")
		return nil, errors.New("target branch advanced after promotion check; run a new check")
	}
	if worktree, checkedOut, err := s.Repo.CheckedOutWorktree(branch); err != nil {
		return nil, err
	} else if checkedOut {
		return nil, fmt.Errorf("target branch is checked out at %s; ref-only promotion is unsafe", worktree)
	}
	newCommit := promotion.ResultCommit
	fastForward, err := s.Repo.IsAncestor(promotion.TargetCommit, promotion.ResultCommit)
	if err != nil {
		return nil, err
	}
	if !fastForward {
		if promotion.MergeTree == "" {
			return nil, errors.New("promotion has no verified merge tree")
		}
		newCommit, err = s.Repo.CommitTree(promotion.MergeTree, []string{promotion.TargetCommit, promotion.ResultCommit}, "bifrost promotion "+promotion.ID)
		if err != nil {
			return nil, err
		}
	}
	if err := s.Store.Update(func(current *model.State) error {
		live := current.Promotion(id)
		if live == nil {
			return fmt.Errorf("promotion %q disappeared", id)
		}
		if live.Status != "ready" {
			return fmt.Errorf("promotion became %s before apply", live.Status)
		}
		live.Status, live.AppliedCommit = "applying", newCommit
		appendEvent(current, bifrostv1.EventPromotionApplying, live.RunID, *live)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.Repo.UpdateRefCAS(branch, newCommit, promotion.TargetCommit, "bifrost: apply "+promotion.ID); err != nil {
		_ = s.markPromotionStale(id, "target branch changed during promotion apply")
		return nil, fmt.Errorf("atomic promotion update failed: %w", err)
	}
	applied, err := s.finalizePromotion(id, newCommit)
	if err != nil {
		return nil, fmt.Errorf("branch was updated to %s but promotion receipt failed: %w", newCommit, err)
	}
	s.recordEvent(bifrostv1.EventPromotionApplied, applied.RunID, *applied)
	return applied, nil
}

func (s *Service) resumePromotion(promotion model.Promotion) (*model.Promotion, error) {
	if promotion.AppliedCommit == "" {
		return nil, errors.New("applying promotion has no candidate commit")
	}
	branch, err := s.Repo.BranchRef(promotion.TargetRef)
	if err != nil {
		return nil, err
	}
	current, err := s.Repo.Resolve(branch)
	if err != nil {
		return nil, err
	}
	if current == promotion.AppliedCommit {
		applied, err := s.finalizePromotion(promotion.ID, promotion.AppliedCommit)
		if err != nil {
			return nil, err
		}
		s.recordEvent(bifrostv1.EventPromotionApplied, applied.RunID, *applied)
		return applied, nil
	}
	if current != promotion.TargetCommit {
		_ = s.markPromotionStale(promotion.ID, "target branch changed during promotion recovery")
		return nil, errors.New("target branch changed during promotion recovery")
	}
	if worktree, checkedOut, err := s.Repo.CheckedOutWorktree(branch); err != nil {
		return nil, err
	} else if checkedOut {
		return nil, fmt.Errorf("target branch is checked out at %s; ref-only promotion is unsafe", worktree)
	}
	if err := s.Repo.UpdateRefCAS(branch, promotion.AppliedCommit, promotion.TargetCommit, "bifrost: recover "+promotion.ID); err != nil {
		_ = s.markPromotionStale(promotion.ID, "target branch changed during promotion recovery")
		return nil, fmt.Errorf("atomic promotion recovery failed: %w", err)
	}
	applied, err := s.finalizePromotion(promotion.ID, promotion.AppliedCommit)
	if err != nil {
		return nil, err
	}
	s.recordEvent(bifrostv1.EventPromotionApplied, applied.RunID, *applied)
	return applied, nil
}

func (s *Service) finalizePromotion(id, commit string) (*model.Promotion, error) {
	var applied model.Promotion
	err := s.Store.Update(func(state *model.State) error {
		promotion := state.Promotion(id)
		if promotion == nil {
			return fmt.Errorf("promotion %q disappeared after ref update", id)
		}
		if promotion.Status != "applying" {
			return fmt.Errorf("promotion became %s while applying", promotion.Status)
		}
		now := time.Now().UTC()
		promotion.Status, promotion.AppliedCommit, promotion.AppliedAt = "applied", commit, &now
		applied = *promotion
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &applied, nil
}

func (s *Service) RemoveWorkspace(runID string, force bool) error {
	state, err := s.Store.Load()
	if err != nil {
		return err
	}
	run := state.Run(runID)
	if run == nil {
		return fmt.Errorf("run %q not found", runID)
	}
	if run.Status == "running" && !force {
		return errors.New("cannot remove an active run workspace without force")
	}
	if err := s.Repo.RemoveWorktree(s.WorkspacePath(*run), force); err != nil {
		return err
	}
	return s.Store.Update(func(current *model.State) error {
		live := current.Run(runID)
		if live == nil {
			return fmt.Errorf("run %q disappeared", runID)
		}
		live.Workspace.State = bifrostv1.WorkspaceRemoved
		appendEvent(current, bifrostv1.EventWorkspaceRemoved, runID, *live)
		return nil
	})
}

func (s *Service) WorkspacePath(run model.Run) string {
	if filepath.IsAbs(run.Workspace.Path) {
		return filepath.Clean(run.Workspace.Path)
	}
	return filepath.Join(s.Store.Root, filepath.FromSlash(run.Workspace.Path))
}

func (s *Service) markPromotionStale(id, reason string) error {
	return s.Store.Update(func(state *model.State) error {
		promotion := state.Promotion(id)
		if promotion == nil {
			return fmt.Errorf("promotion %q not found", id)
		}
		promotion.Status = "stale"
		promotion.PolicyFailures = append(promotion.PolicyFailures, reason)
		appendEvent(state, bifrostv1.EventPromotionStale, promotion.RunID, *promotion)
		return nil
	})
}

func (s *Service) recordEvent(eventType bifrostv1.EventType, runID string, data any) {
	_ = s.Store.Update(func(state *model.State) error {
		appendEvent(state, eventType, runID, data)
		return nil
	})
}

func appendEvent(state *model.State, eventType bifrostv1.EventType, runID string, data any) {
	payload, _ := json.Marshal(data)
	var sequence uint64
	if len(state.Events) > 0 {
		sequence = state.Events[len(state.Events)-1].Sequence
	}
	state.Events = append(state.Events, model.Event{ID: store.NewID("evt"), Type: eventType, RepositoryID: state.Repository.ID, RunID: runID, Sequence: sequence + 1, OccurredAt: time.Now().UTC(), Data: payload})
}

func latestRevision(state *model.State, run *model.Run) string {
	if checkpoint := latestCheckpoint(state, run.ID); checkpoint != "" {
		return checkpoint
	}
	if run.ResultCommit != "" {
		return run.ResultCommit
	}
	return run.BaseCommit
}

func latestCheckpoint(state *model.State, runID string) string {
	result := ""
	for _, checkpoint := range state.Checkpoints {
		if checkpoint.RunID == runID {
			result = checkpoint.Commit
		}
	}
	return result
}

func hasPassingTest(state *model.State, runID string) bool {
	for _, evidence := range state.Evidence {
		if evidence.RunID == runID && evidence.Kind == "test" && evidence.ExitCode == 0 {
			return true
		}
	}
	return false
}

func checkpointsFor(state *model.State, runID string) []model.Checkpoint {
	result := []model.Checkpoint{}
	for _, checkpoint := range state.Checkpoints {
		if checkpoint.RunID == runID {
			result = append(result, checkpoint)
		}
	}
	return result
}

func evidenceFor(state *model.State, runID string) []model.Evidence {
	result := []model.Evidence{}
	for _, evidence := range state.Evidence {
		if evidence.RunID == runID {
			result = append(result, evidence)
		}
	}
	return result
}

func promotionsFor(state *model.State, runID string) []model.Promotion {
	result := []model.Promotion{}
	for _, promotion := range state.Promotions {
		if promotion.RunID == runID {
			result = append(result, promotion)
		}
	}
	return result
}
