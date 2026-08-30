package v1

import (
	"encoding/json"
	"time"
)

type EventType string

const (
	EventRunCreated        EventType = "run.created"
	EventRunUpdated        EventType = "run.updated"
	EventCheckpointCreated EventType = "checkpoint.created"
	EventEvidenceCreated   EventType = "evidence.created"
	EventPromotionChecked  EventType = "promotion.checked"
	EventPromotionApplying EventType = "promotion.applying"
	EventPromotionApplied  EventType = "promotion.applied"
	EventPromotionStale    EventType = "promotion.stale"
	EventWorkspaceRemoved  EventType = "workspace.removed"
)

// Event is an ordered repository event. Sequence is monotonically increasing
// within one repository; consumers use ID for deduplication and resume after
// the last observed sequence.
type Event struct {
	ID           string          `json:"id"`
	Type         EventType       `json:"type"`
	RepositoryID string          `json:"repository_id"`
	RunID        string          `json:"run_id,omitempty"`
	Sequence     uint64          `json:"sequence"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Data         json.RawMessage `json:"data"`
}

// EventsQuery is the wire representation of a replay cursor. Events after
// After are returned in sequence order; a live stream keeps the connection
// open until the caller cancels it.
type EventsQuery struct {
	After uint64 `json:"after,omitempty"`
}
