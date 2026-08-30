package backend

import (
	"context"

	"github.com/CytieqLabs/Bifrost/client/internal/service"
	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

type Local struct{ Service *service.Service }

func OpenLocal(dir string) (*Local, error) {
	app, err := service.Open(dir)
	if err != nil {
		return nil, err
	}
	return &Local{Service: app}, nil
}

func (l *Local) Kind() string { return "local" }

func (l *Local) Status(ctx context.Context) (*bifrostv1.Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	status, err := l.Service.Status()
	return &status, err
}

func (l *Local) Runs(ctx context.Context) ([]bifrostv1.Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return l.Service.Runs()
}

func (l *Local) StartRun(ctx context.Context, input bifrostv1.StartRunRequest) (*bifrostv1.Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return l.Service.StartRun(input.Task, input.Agent, input.ParentID)
}

func (l *Local) Run(ctx context.Context, id string) (*bifrostv1.RunDetails, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return l.Service.Run(id)
}

func (l *Local) CreateCheckpoint(ctx context.Context, id string, input bifrostv1.CreateCheckpointRequest) (*bifrostv1.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return l.Service.CreateCheckpoint(id, input.Note)
}

func (l *Local) AddEvidence(ctx context.Context, id string, input bifrostv1.AddEvidenceRequest) (*bifrostv1.Evidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return l.Service.AddEvidence(id, input)
}

func (l *Local) FinishRun(ctx context.Context, id string, input bifrostv1.FinishRunRequest) (*bifrostv1.Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return l.Service.FinishRun(id, string(input.Status), input.Result)
}

func (l *Local) CheckPromotion(ctx context.Context, id string, input bifrostv1.CheckPromotionRequest) (*bifrostv1.Promotion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return l.Service.CheckPromotion(id, input.Target)
}

func (l *Local) ApplyPromotion(ctx context.Context, id string) (*bifrostv1.Promotion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return l.Service.ApplyPromotion(id)
}

func (l *Local) WorkspacePath(run bifrostv1.Run) string { return l.Service.WorkspacePath(run) }

func (l *Local) RemoveWorkspace(ctx context.Context, id string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.Service.RemoveWorkspace(id, force)
}
