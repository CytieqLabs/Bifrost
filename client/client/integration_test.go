package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	internalapi "github.com/CytieqLabs/Bifrost/client/internal/api"
	"github.com/CytieqLabs/Bifrost/client/internal/service"
	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

func TestSDKMatchesEmbeddedV1Handler(t *testing.T) {
	app := integrationService(t)
	handler := &internalapi.Handler{Service: app, Token: "secret"}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
	sdk, err := New("http://bifrost.local", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	run, err := sdk.StartRun(context.Background(), bifrostv1.StartRunRequest{Task: "contract test", Agent: "isha"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Workspace.Mode != bifrostv1.WorkspaceLocal || run.Workspace.ID == "" {
		t.Fatalf("unexpected workspace contract: %#v", run.Workspace)
	}
	details, err := sdk.Run(context.Background(), run.ID)
	if err != nil || details.Run.ID != run.ID {
		t.Fatalf("run round trip failed: %#v, %v", details, err)
	}
}

func integrationService(t *testing.T) *service.Service {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "app.txt")
	runGit(t, root, "commit", "-qm", "base")
	app, err := service.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Initialize(); err != nil {
		t.Fatal(err)
	}
	return app
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
