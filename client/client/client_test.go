package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

func TestStartRunUsesVersionedProtocol(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != bifrostv1.RunsPath {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get(bifrostv1.HeaderAuthorization) != bifrostv1.BearerPrefix+"secret" {
			t.Fatal("missing bearer token")
		}
		var input bifrostv1.StartRunRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(bifrostv1.Run{ID: "run-1", Task: input.Task, Agent: input.Agent})
		return response(http.StatusOK, data), nil
	})}

	client, err := New("https://bifrost.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	run, err := client.StartRun(context.Background(), bifrostv1.StartRunRequest{Task: "work", Agent: "isha"})
	if err != nil || run.ID != "run-1" {
		t.Fatalf("unexpected run: %#v, %v", run, err)
	}
}

func TestProtocolErrorIsTyped(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		data, _ := json.Marshal(bifrostv1.ErrorEnvelope{Error: bifrostv1.Error{Code: bifrostv1.ErrorConflict, Message: "target advanced"}})
		return response(http.StatusConflict, data), nil
	})}

	client, _ := New("https://bifrost.example", "", httpClient)
	_, err := client.ApplyPromotion(context.Background(), "prom-1")
	var httpError *HTTPError
	if !errors.As(err, &httpError) || httpError.StatusCode != http.StatusConflict || httpError.Code != bifrostv1.ErrorConflict {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestMutationCarriesIdempotencyKey(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get(bifrostv1.HeaderIdempotencyKey) != "operation-123" {
			t.Fatalf("idempotency key missing: %#v", request.Header)
		}
		data, _ := json.Marshal(bifrostv1.Run{ID: "run-1"})
		return response(http.StatusCreated, data), nil
	})}
	client, err := New("https://bifrost.example", "", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithIdempotencyKey(context.Background(), "operation-123")
	if _, err := client.StartRun(ctx, bifrostv1.StartRunRequest{Task: "work", Agent: "isha"}); err != nil {
		t.Fatal(err)
	}
}

func TestStreamEventsReplaysSSE(t *testing.T) {
	event := bifrostv1.Event{ID: "evt-1", Type: bifrostv1.EventRunCreated, Sequence: 3}
	data, _ := json.Marshal(event)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != bifrostv1.EventsPath || request.URL.Query().Get("after") != "2" {
			t.Fatalf("unexpected event request: %s?%s", request.URL.Path, request.URL.RawQuery)
		}
		body := "id: evt-1\nevent: run.created\ndata: " + string(data) + "\n\n"
		return response(http.StatusOK, []byte(body)), nil
	})}
	client, err := New("https://bifrost.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	var received bifrostv1.Event
	if err := client.StreamEvents(context.Background(), 2, func(event bifrostv1.Event) error {
		received = event
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if received.ID != event.ID || received.Sequence != event.Sequence {
		t.Fatalf("event was not decoded: %#v", received)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func response(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func TestEndpointMustBeAnOrigin(t *testing.T) {
	for _, endpoint := range []string{"localhost:8741", "ftp://example.com", "https://user@example.com", "https://example.com/v1"} {
		if _, err := New(endpoint, "", nil); err == nil {
			t.Fatalf("invalid endpoint accepted: %s", endpoint)
		}
	}
}
