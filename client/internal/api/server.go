package api

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CytieqLabs/Bifrost/client/internal/model"
	"github.com/CytieqLabs/Bifrost/client/internal/service"
	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

const maxRequestBody = 1 << 20

type Handler struct {
	Service       *service.Service
	Token         string
	idempotencyMu sync.Mutex
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.Header.Get(bifrostv1.HeaderIdempotencyKey) != "" && (h.Token == "" || validBearer(r.Header.Get(bifrostv1.HeaderAuthorization), h.Token)) {
		h.serveIdempotent(w, r)
		return
	}
	h.serve(w, r)
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", bifrostv1.MediaTypeJSON)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path == bifrostv1.HealthPath && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": bifrostv1.Version})
		return
	}
	if h.Token != "" && !validBearer(r.Header.Get(bifrostv1.HeaderAuthorization), h.Token) {
		writeError(w, http.StatusUnauthorized, bifrostv1.ErrorUnauthorized, "a valid bearer token is required")
		return
	}
	if r.URL.Path == bifrostv1.StatusPath && r.Method == http.MethodGet {
		status, err := h.Service.Status()
		h.respond(w, status, err)
		return
	}
	if r.URL.Path == bifrostv1.EventsPath && r.Method == http.MethodGet {
		h.serveEvents(w, r)
		return
	}
	if r.URL.Path == bifrostv1.RunsPath {
		switch r.Method {
		case http.MethodGet:
			runs, err := h.Service.Runs()
			h.respond(w, runs, err)
		case http.MethodPost:
			var input bifrostv1.StartRunRequest
			if !decode(w, r, &input) {
				return
			}
			run, err := h.Service.StartRun(input.Task, input.Agent, input.ParentID)
			h.respondStatus(w, http.StatusCreated, run, err)
		default:
			methodNotAllowed(w)
		}
		return
	}
	segments := splitPath(r.URL.Path)
	if len(segments) >= 3 && segments[0] == "v1" && segments[1] == "runs" {
		h.handleRun(w, r, segments[2:])
		return
	}
	if len(segments) == 4 && segments[0] == "v1" && segments[1] == "promotions" && segments[3] == "apply" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		promotion, err := h.Service.ApplyPromotion(segments[2])
		h.respond(w, promotion, err)
		return
	}
	writeError(w, http.StatusNotFound, bifrostv1.ErrorNotFound, "endpoint not found")
}

func (h *Handler) serveEvents(w http.ResponseWriter, r *http.Request) {
	after := uint64(0)
	if value := r.URL.Query().Get("after"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, bifrostv1.ErrorInvalidRequest, "after must be an unsigned sequence")
			return
		}
		after = parsed
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, bifrostv1.ErrorInvalidRequest, "streaming is unavailable")
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := h.Service.Store.Load()
		if err != nil {
			return
		}
		for _, event := range state.Events {
			if event.Sequence <= after {
				continue
			}
			payload, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Type, payload)
			after = event.Sequence
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *Handler) serveIdempotent(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get(bifrostv1.HeaderIdempotencyKey)
	if !validIdempotencyKey(key) {
		writeError(w, http.StatusBadRequest, bifrostv1.ErrorInvalidIdempotency, "Idempotency-Key must be 8-200 visible ASCII characters")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, bifrostv1.ErrorInvalidJSON, err.Error())
		return
	}
	if len(body) > maxRequestBody {
		writeError(w, http.StatusRequestEntityTooLarge, bifrostv1.ErrorRequestTooLarge, "request body exceeds 1 MiB")
		return
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(r.Method + "\n" + r.URL.Path + "\n"))
	_, _ = hash.Write(body)
	fingerprint := fmt.Sprintf("%x", hash.Sum(nil))

	// The process-local lock closes the duplicate window for the embedded
	// service. A hosted implementation will enforce the same invariant in its
	// database/lease layer.
	h.idempotencyMu.Lock()
	defer h.idempotencyMu.Unlock()
	state, err := h.Service.Store.Load()
	if err == nil {
		if receipt, ok := state.Idempotency[key]; ok {
			if receipt.Fingerprint != fingerprint {
				writeError(w, http.StatusConflict, bifrostv1.ErrorIdempotencyConflict, "Idempotency-Key was already used for a different request")
				return
			}
			replay(w, receipt.Status, receipt.Body)
			return
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	capture := newCaptureWriter()
	h.serve(capture, r)
	copyHeaders(w.Header(), capture.Header())
	w.WriteHeader(capture.status)
	_, _ = w.Write(capture.body.Bytes())
	if capture.status >= 200 && capture.status < 500 && err == nil {
		_ = h.Service.Store.Update(func(current *model.State) error {
			if current.Idempotency == nil {
				current.Idempotency = map[string]model.IdempotencyReceipt{}
			}
			current.Idempotency[key] = model.IdempotencyReceipt{Fingerprint: fingerprint, Status: capture.status, Body: append([]byte(nil), capture.body.Bytes()...), CreatedAt: time.Now().UTC()}
			return nil
		})
	}
}

type captureWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newCaptureWriter() *captureWriter       { return &captureWriter{header: make(http.Header)} }
func (w *captureWriter) Header() http.Header { return w.header }
func (w *captureWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *captureWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

func replay(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set(bifrostv1.HeaderIdempotencyReplay, "true")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func copyHeaders(to, from http.Header) {
	for key, values := range from {
		to.Del(key)
		for _, value := range values {
			to.Add(key, value)
		}
	}
}

func validIdempotencyKey(key string) bool {
	if len(key) < 8 || len(key) > 200 {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

func (h *Handler) handleRun(w http.ResponseWriter, r *http.Request, segments []string) {
	runID := segments[0]
	if len(segments) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		details, err := h.Service.Run(runID)
		h.respond(w, details, err)
		return
	}
	if len(segments) != 2 || r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	switch segments[1] {
	case "checkpoints":
		var input bifrostv1.CreateCheckpointRequest
		if !decode(w, r, &input) {
			return
		}
		checkpoint, err := h.Service.CreateCheckpoint(runID, input.Note)
		h.respondStatus(w, http.StatusCreated, checkpoint, err)
	case "evidence":
		var input bifrostv1.AddEvidenceRequest
		if !decode(w, r, &input) {
			return
		}
		evidence, err := h.Service.AddEvidence(runID, input)
		h.respondStatus(w, http.StatusCreated, evidence, err)
	case "finish":
		var input bifrostv1.FinishRunRequest
		if !decode(w, r, &input) {
			return
		}
		run, err := h.Service.FinishRun(runID, string(input.Status), input.Result)
		h.respond(w, run, err)
	case "promotions":
		var input bifrostv1.CheckPromotionRequest
		if !decode(w, r, &input) {
			return
		}
		promotion, err := h.Service.CheckPromotion(runID, input.Target)
		h.respondStatus(w, http.StatusCreated, promotion, err)
	default:
		writeError(w, http.StatusNotFound, bifrostv1.ErrorNotFound, "endpoint not found")
	}
}

func (h *Handler) respond(w http.ResponseWriter, value any, err error) {
	h.respondStatus(w, http.StatusOK, value, err)
}

func (h *Handler) respondStatus(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		code, errorCode := http.StatusBadRequest, bifrostv1.ErrorInvalidRequest
		message := err.Error()
		if strings.Contains(message, "not found") {
			code, errorCode = http.StatusNotFound, bifrostv1.ErrorNotFound
		} else if strings.Contains(message, "already") || strings.Contains(message, "advanced") || strings.Contains(message, "not ready") || strings.Contains(message, "concurrently") || strings.Contains(message, "checked out") {
			code, errorCode = http.StatusConflict, bifrostv1.ErrorConflict
		}
		writeError(w, code, errorCode, message)
		return
	}
	writeJSON(w, status, value)
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if contentType := r.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(contentType, bifrostv1.MediaTypeJSON) {
		writeError(w, http.StatusUnsupportedMediaType, bifrostv1.ErrorUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, bifrostv1.ErrorRequestTooLarge, "request body exceeds 1 MiB")
			return false
		}
		writeError(w, http.StatusBadRequest, bifrostv1.ErrorInvalidJSON, err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, bifrostv1.ErrorInvalidJSON, "request body must contain one JSON object")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	response := bifrostv1.ErrorEnvelope{Error: bifrostv1.Error{Code: code, Message: message}}
	writeJSON(w, status, response)
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, bifrostv1.ErrorMethodNotAllowed, "method not allowed")
}

func validBearer(header, token string) bool {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return false
	}
	provided, expected := sha256.Sum256([]byte(parts[1])), sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(provided[:], expected[:]) == 1
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func NewServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func ValidateAddress(address, token string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if token == "" && !loopback {
		return fmt.Errorf("a bearer token is required when listening beyond loopback")
	}
	return nil
}
