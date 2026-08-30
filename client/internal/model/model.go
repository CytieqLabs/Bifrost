package model

import (
	"time"

	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

const SchemaVersion = 2

type Run = bifrostv1.Run
type Checkpoint = bifrostv1.Checkpoint
type Evidence = bifrostv1.Evidence
type Promotion = bifrostv1.Promotion
type Event = bifrostv1.Event

type Repository struct {
	ID        string    `json:"id"`
	Root      string    `json:"root"`
	Remote    string    `json:"remote,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type State struct {
	Version     int                           `json:"version"`
	Repository  Repository                    `json:"repository"`
	Runs        []Run                         `json:"runs"`
	Checkpoints []Checkpoint                  `json:"checkpoints"`
	Evidence    []Evidence                    `json:"evidence"`
	Promotions  []Promotion                   `json:"promotions"`
	Events      []Event                       `json:"events"`
	Idempotency map[string]IdempotencyReceipt `json:"idempotency,omitempty"`
}

type IdempotencyReceipt struct {
	Fingerprint string    `json:"fingerprint"`
	Status      int       `json:"status"`
	Body        []byte    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *State) Run(id string) *Run {
	for i := range s.Runs {
		if s.Runs[i].ID == id {
			return &s.Runs[i]
		}
	}
	return nil
}

func (s *State) Promotion(id string) *Promotion {
	for i := range s.Promotions {
		if s.Promotions[i].ID == id {
			return &s.Promotions[i]
		}
	}
	return nil
}
