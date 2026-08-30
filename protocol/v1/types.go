package v1

import "time"

const Version = "v1"

type RunStatus string

const (
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

type EvidenceKind string

const (
	EvidenceTest   EvidenceKind = "test"
	EvidenceBuild  EvidenceKind = "build"
	EvidenceScan   EvidenceKind = "scan"
	EvidenceReview EvidenceKind = "review"
)

type PromotionStatus string

const (
	PromotionReady      PromotionStatus = "ready"
	PromotionBlocked    PromotionStatus = "blocked"
	PromotionStale      PromotionStatus = "stale"
	PromotionConflicted PromotionStatus = "conflicted"
	PromotionApplying   PromotionStatus = "applying"
	PromotionApplied    PromotionStatus = "applied"
)

type WorkspaceMode string

const (
	WorkspaceLocal  WorkspaceMode = "local"
	WorkspaceRemote WorkspaceMode = "remote"
)

type WorkspaceState string

const (
	WorkspaceActive  WorkspaceState = "active"
	WorkspaceRemoved WorkspaceState = "removed"
)

type Workspace struct {
	ID    string         `json:"id"`
	Mode  WorkspaceMode  `json:"mode"`
	State WorkspaceState `json:"state"`
	Path  string         `json:"path,omitempty"`
}

type Run struct {
	ID           string    `json:"id"`
	Task         string    `json:"task"`
	Agent        string    `json:"agent"`
	ParentID     string    `json:"parent_id,omitempty"`
	BaseCommit   string    `json:"base_commit"`
	ResultCommit string    `json:"result_commit,omitempty"`
	Workspace    Workspace `json:"workspace"`
	Status       RunStatus `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Checkpoint struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	Commit    string    `json:"commit"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Evidence struct {
	ID             string       `json:"id"`
	RunID          string       `json:"run_id"`
	Kind           EvidenceKind `json:"kind"`
	Command        string       `json:"command,omitempty"`
	ExitCode       int          `json:"exit_code"`
	Summary        string       `json:"summary,omitempty"`
	Artifact       string       `json:"artifact,omitempty"`
	ArtifactSHA256 string       `json:"artifact_sha256,omitempty"`
	CreatedAt      time.Time    `json:"created_at"`
}

type Promotion struct {
	ID             string          `json:"id"`
	RunID          string          `json:"run_id"`
	TargetRef      string          `json:"target_ref"`
	TargetCommit   string          `json:"target_commit"`
	ResultCommit   string          `json:"result_commit"`
	MergeTree      string          `json:"merge_tree,omitempty"`
	Status         PromotionStatus `json:"status"`
	PolicyFailures []string        `json:"policy_failures,omitempty"`
	Conflicts      []string        `json:"conflicts,omitempty"`
	AppliedCommit  string          `json:"applied_commit,omitempty"`
	AppliedAt      *time.Time      `json:"applied_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

type Status struct {
	Repository  string `json:"repository"`
	Head        string `json:"head"`
	Dirty       bool   `json:"dirty"`
	Runs        int    `json:"runs"`
	ActiveRuns  int    `json:"active_runs"`
	Checkpoints int    `json:"checkpoints"`
	Evidence    int    `json:"evidence"`
	Promotions  int    `json:"promotions"`
}

type RunDetails struct {
	Run         Run          `json:"run"`
	Checkpoints []Checkpoint `json:"checkpoints"`
	Evidence    []Evidence   `json:"evidence"`
	Promotions  []Promotion  `json:"promotions"`
}
