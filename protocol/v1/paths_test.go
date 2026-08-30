package v1

import "testing"

func TestPathsEscapeIdentifiers(t *testing.T) {
	if got := RunPath("run/a b"); got != "/v1/runs/run%2Fa%20b" {
		t.Fatalf("unexpected run path: %s", got)
	}
	if got := PromotionApplyPath("prom/a"); got != "/v1/promotions/prom%2Fa/apply" {
		t.Fatalf("unexpected promotion path: %s", got)
	}
	if EventsPath != "/v1/events" {
		t.Fatalf("unexpected events path: %s", EventsPath)
	}
}
