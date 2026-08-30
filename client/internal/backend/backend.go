package backend

import (
	"context"

	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

type Backend interface {
	Kind() string
	Status(context.Context) (*bifrostv1.Status, error)
	Runs(context.Context) ([]bifrostv1.Run, error)
	StartRun(context.Context, bifrostv1.StartRunRequest) (*bifrostv1.Run, error)
	Run(context.Context, string) (*bifrostv1.RunDetails, error)
	CreateCheckpoint(context.Context, string, bifrostv1.CreateCheckpointRequest) (*bifrostv1.Checkpoint, error)
	AddEvidence(context.Context, string, bifrostv1.AddEvidenceRequest) (*bifrostv1.Evidence, error)
	FinishRun(context.Context, string, bifrostv1.FinishRunRequest) (*bifrostv1.Run, error)
	CheckPromotion(context.Context, string, bifrostv1.CheckPromotionRequest) (*bifrostv1.Promotion, error)
	ApplyPromotion(context.Context, string) (*bifrostv1.Promotion, error)
}

type LocalWorkspace interface {
	WorkspacePath(bifrostv1.Run) string
	RemoveWorkspace(context.Context, string, bool) error
}
