package v1

import "net/url"

const (
	BasePath   = "/v1"
	HealthPath = BasePath + "/health"
	StatusPath = BasePath + "/status"
	RunsPath   = BasePath + "/runs"
	EventsPath = BasePath + "/events"
)

func RunPath(runID string) string {
	return RunsPath + "/" + url.PathEscape(runID)
}

func RunCheckpointsPath(runID string) string {
	return RunPath(runID) + "/checkpoints"
}

func RunEvidencePath(runID string) string {
	return RunPath(runID) + "/evidence"
}

func RunFinishPath(runID string) string {
	return RunPath(runID) + "/finish"
}

func RunPromotionsPath(runID string) string {
	return RunPath(runID) + "/promotions"
}

func PromotionApplyPath(promotionID string) string {
	return BasePath + "/promotions/" + url.PathEscape(promotionID) + "/apply"
}
