package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CytieqLabs/Bifrost/client/internal/service"
	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

func TestV1AuthenticationAndRunCreation(t *testing.T) {
	app := initializedService(t)
	handler := &Handler{Service: app, Token: "secret-token"}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health should not require authentication: %d", health.Code)
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthorized.Code)
	}

	invalid := request(http.MethodPost, "/v1/runs", `{"task":"work","agent":"isha","unexpected":true}`, "secret-token")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown JSON fields should fail: %d %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	oversized := request(http.MethodPost, "/v1/runs", `{"task":"`+strings.Repeat("x", maxRequestBody)+`","agent":"isha"}`, "secret-token")
	oversizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(oversizedResponse, oversized)
	if oversizedResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request should fail with 413: %d", oversizedResponse.Code)
	}

	create := request(http.MethodPost, "/v1/runs", `{"task":"work","agent":"isha"}`, "secret-token")
	create.Header.Set(bifrostv1.HeaderIdempotencyKey, "create-run-1")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("run creation failed: %d %s", created.Code, created.Body.String())
	}
	var run bifrostv1.Run
	if err := json.NewDecoder(created.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if run.ID == "" || run.Workspace.ID == "" || run.Workspace.Mode != bifrostv1.WorkspaceLocal {
		t.Fatalf("incomplete run response: %#v", run)
	}
	retry := request(http.MethodPost, "/v1/runs", `{"task":"work","agent":"isha"}`, "secret-token")
	retry.Header.Set(bifrostv1.HeaderIdempotencyKey, "create-run-1")
	retried := httptest.NewRecorder()
	(&Handler{Service: app, Token: "secret-token"}).ServeHTTP(retried, retry)
	if retried.Code != http.StatusCreated || retried.Header().Get(bifrostv1.HeaderIdempotencyReplay) != "true" {
		t.Fatalf("retry was not replayed: %d %#v", retried.Code, retried.Header())
	}
	var replayed bifrostv1.Run
	if err := json.NewDecoder(retried.Body).Decode(&replayed); err != nil || replayed.ID != run.ID {
		t.Fatalf("replayed response changed: %#v, %v", replayed, err)
	}
	runs, err := app.Runs()
	if err != nil || len(runs) != 1 {
		t.Fatalf("idempotent retry created duplicate run: %d, %v", len(runs), err)
	}
	conflicting := request(http.MethodPost, "/v1/runs", `{"task":"different","agent":"isha"}`, "secret-token")
	conflicting.Header.Set(bifrostv1.HeaderIdempotencyKey, "create-run-1")
	conflict := httptest.NewRecorder()
	handler.ServeHTTP(conflict, conflicting)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("key reuse with different body was accepted: %d", conflict.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	eventRequest := httptest.NewRequest(http.MethodGet, bifrostv1.EventsPath+"?after=0", nil).WithContext(ctx)
	eventRequest.Header.Set(bifrostv1.HeaderAuthorization, bifrostv1.BearerPrefix+"secret-token")
	eventResponse := httptest.NewRecorder()
	handler.ServeHTTP(eventResponse, eventRequest)
	if eventResponse.Code != http.StatusOK || !strings.Contains(eventResponse.Body.String(), "run.created") {
		t.Fatalf("event replay failed: %d %s", eventResponse.Code, eventResponse.Body.String())
	}
}

func TestValidateAddressRequiresTokenOffLoopback(t *testing.T) {
	if err := ValidateAddress("127.0.0.1:8741", ""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAddress("[::1]:8741", ""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAddress("0.0.0.0:8741", ""); err == nil {
		t.Fatal("public bind without a token was accepted")
	}
	if err := ValidateAddress("0.0.0.0:8741", "secret"); err != nil {
		t.Fatal(err)
	}
}

func initializedService(t *testing.T) *service.Service {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q")
	git(t, root, "config", "user.name", "Test")
	git(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "app.txt")
	git(t, root, "commit", "-qm", "base")
	app, err := service.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Initialize(); err != nil {
		t.Fatal(err)
	}
	return app
}

func request(method, path, body, token string) *http.Request {
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", bifrostv1.MediaTypeJSON)
	r.Header.Set(bifrostv1.HeaderAuthorization, bifrostv1.BearerPrefix+token)
	return r
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
