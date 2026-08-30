package backend

import (
	"context"
	"net/http"

	httpclient "github.com/CytieqLabs/Bifrost/client/client"
	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

type Remote struct{ Client *httpclient.Client }

func OpenRemote(endpoint, token string, client *http.Client) (*Remote, error) {
	api, err := httpclient.New(endpoint, token, client)
	if err != nil {
		return nil, err
	}
	return &Remote{Client: api}, nil
}

func (r *Remote) Kind() string { return "remote" }

func (r *Remote) Status(ctx context.Context) (*bifrostv1.Status, error) {
	return r.Client.Status(ctx)
}
func (r *Remote) Runs(ctx context.Context) ([]bifrostv1.Run, error) {
	return r.Client.Runs(ctx)
}
func (r *Remote) StartRun(ctx context.Context, input bifrostv1.StartRunRequest) (*bifrostv1.Run, error) {
	return r.Client.StartRun(ctx, input)
}
func (r *Remote) Run(ctx context.Context, id string) (*bifrostv1.RunDetails, error) {
	return r.Client.Run(ctx, id)
}
func (r *Remote) CreateCheckpoint(ctx context.Context, id string, input bifrostv1.CreateCheckpointRequest) (*bifrostv1.Checkpoint, error) {
	return r.Client.CreateCheckpoint(ctx, id, input)
}
func (r *Remote) AddEvidence(ctx context.Context, id string, input bifrostv1.AddEvidenceRequest) (*bifrostv1.Evidence, error) {
	return r.Client.AddEvidence(ctx, id, input)
}
func (r *Remote) FinishRun(ctx context.Context, id string, input bifrostv1.FinishRunRequest) (*bifrostv1.Run, error) {
	return r.Client.FinishRun(ctx, id, input)
}
func (r *Remote) CheckPromotion(ctx context.Context, id string, input bifrostv1.CheckPromotionRequest) (*bifrostv1.Promotion, error) {
	return r.Client.CheckPromotion(ctx, id, input)
}
func (r *Remote) ApplyPromotion(ctx context.Context, id string) (*bifrostv1.Promotion, error) {
	return r.Client.ApplyPromotion(ctx, id)
}
