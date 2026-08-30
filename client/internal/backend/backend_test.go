package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

func TestRemoteBackendUsesProtocolClient(t *testing.T) {
	httpClient := &http.Client{Transport: transportFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != bifrostv1.StatusPath || request.Header.Get(bifrostv1.HeaderAuthorization) != bifrostv1.BearerPrefix+"secret" {
			t.Fatalf("unexpected request: %s %#v", request.URL.Path, request.Header)
		}
		data, _ := json.Marshal(bifrostv1.Status{Repository: "cloud-repo", Runs: 4})
		return &http.Response{StatusCode: http.StatusOK, Status: "OK", Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	remote, err := OpenRemote("https://bifrost.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	var selected Backend = remote
	status, err := selected.Status(context.Background())
	if err != nil || status.Repository != "cloud-repo" || status.Runs != 4 || selected.Kind() != "remote" {
		t.Fatalf("unexpected remote status: %#v, %v", status, err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

var _ Backend = (*Local)(nil)
var _ Backend = (*Remote)(nil)
var _ LocalWorkspace = (*Local)(nil)
