package v1

const (
	MediaTypeJSON           = "application/json"
	HeaderAuthorization     = "Authorization"
	BearerPrefix            = "Bearer "
	HeaderIdempotencyKey    = "Idempotency-Key"
	HeaderIdempotencyReplay = "Idempotency-Replayed"

	ErrorUnauthorized         = "unauthorized"
	ErrorNotFound             = "not_found"
	ErrorInvalidRequest       = "invalid_request"
	ErrorInvalidJSON          = "invalid_json"
	ErrorConflict             = "conflict"
	ErrorMethodNotAllowed     = "method_not_allowed"
	ErrorUnsupportedMediaType = "unsupported_media_type"
	ErrorRequestTooLarge      = "request_too_large"
	ErrorIdempotencyConflict  = "idempotency_conflict"
	ErrorInvalidIdempotency   = "invalid_idempotency_key"
)

type StartRunRequest struct {
	Task     string `json:"task"`
	Agent    string `json:"agent"`
	ParentID string `json:"parent_id,omitempty"`
}

type CreateCheckpointRequest struct {
	Note string `json:"note,omitempty"`
}

type AddEvidenceRequest struct {
	Kind     EvidenceKind `json:"kind"`
	Command  string       `json:"command,omitempty"`
	ExitCode int          `json:"exit_code"`
	Summary  string       `json:"summary,omitempty"`
	Artifact string       `json:"artifact,omitempty"`
}

type FinishRunRequest struct {
	Status RunStatus `json:"status,omitempty"`
	Result string    `json:"result,omitempty"`
}

type CheckPromotionRequest struct {
	Target string `json:"target,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorEnvelope struct {
	Error Error `json:"error"`
}
