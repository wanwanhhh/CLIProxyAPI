package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
)

type transparentWebsocketSelector struct {
	mu     sync.Mutex
	order  []string
	cursor int
}

func (s *transparentWebsocketSelector) Pick(_ context.Context, _ string, _ string, _ cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(auths) == 0 {
		return nil, errors.New("no auth available")
	}
	for s.cursor < len(s.order) {
		authID := strings.TrimSpace(s.order[s.cursor])
		s.cursor++
		for _, auth := range auths {
			if auth != nil && auth.ID == authID {
				return auth, nil
			}
		}
	}
	return auths[0], nil
}

type transparentWebsocketCaptureExecutor struct {
	provider string
	mu       sync.Mutex
	authIDs  []string
}

func (e *transparentWebsocketCaptureExecutor) Identifier() string { return e.provider }

func (e *transparentWebsocketCaptureExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, errors.New("not implemented")
}

func (e *transparentWebsocketCaptureExecutor) ExecuteStream(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	e.mu.Lock()
	if auth != nil {
		e.authIDs = append(e.authIDs, auth.ID)
	}
	e.mu.Unlock()

	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("ok")}
	close(chunks)
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}

func (e *transparentWebsocketCaptureExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *transparentWebsocketCaptureExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, errors.New("not implemented")
}

func (e *transparentWebsocketCaptureExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

func (e *transparentWebsocketCaptureExecutor) AuthIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.authIDs...)
}

func TestManagerExecuteStreamTransparentWebsocketReturnsExplicitErrorWhenNoValidCodexWebsocketAuth(t *testing.T) {
	selector := &transparentWebsocketSelector{order: []string{"auth-openai", "auth-codex-http"}}
	manager := NewManager(nil, selector, nil)

	openaiExec := &transparentWebsocketCaptureExecutor{provider: "openai"}
	codexExec := &transparentWebsocketCaptureExecutor{provider: "codex"}
	manager.RegisterExecutor(openaiExec)
	manager.RegisterExecutor(codexExec)

	authOpenAI := &Auth{ID: "auth-openai", Provider: "openai", Status: StatusActive}
	authCodexHTTP := &Auth{ID: "auth-codex-http", Provider: "codex", Status: StatusActive}
	if _, err := manager.Register(context.Background(), authOpenAI); err != nil {
		t.Fatalf("register openai auth: %v", err)
	}
	if _, err := manager.Register(context.Background(), authCodexHTTP); err != nil {
		t.Fatalf("register codex auth: %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(authOpenAI.ID, authOpenAI.Provider, []*registry.ModelInfo{{ID: "gpt-5"}})
	registry.GetGlobalRegistry().RegisterClient(authCodexHTTP.ID, authCodexHTTP.Provider, []*registry.ModelInfo{{ID: "gpt-5"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(authOpenAI.ID)
		registry.GetGlobalRegistry().UnregisterClient(authCodexHTTP.ID)
	})

	_, err := manager.ExecuteStream(context.Background(), []string{"openai", "codex"}, cliproxyexecutor.Request{
		Model:   "gpt-5",
		Payload: []byte(`{"model":"gpt-5","input":[]}`),
	}, cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.TransparentWebsocketModeMetadataKey: true,
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if status := err.(interface{ StatusCode() int }).StatusCode(); status != http.StatusConflict {
		t.Fatalf("status = %d, want %d", status, http.StatusConflict)
	}
	if got := gjson.Get(err.Error(), "error.code").String(); got != "transparent_websocket_auth_required" {
		t.Fatalf("error.code = %q, want %q", got, "transparent_websocket_auth_required")
	}
	if got := gjson.Get(err.Error(), "error.type").String(); got != "invalid_request_error" {
		t.Fatalf("error.type = %q, want %q", got, "invalid_request_error")
	}
	if calls := len(openaiExec.AuthIDs()); calls != 0 {
		t.Fatalf("openai executor calls = %d, want 0", calls)
	}
	if calls := len(codexExec.AuthIDs()); calls != 0 {
		t.Fatalf("codex executor calls = %d, want 0", calls)
	}
}

func TestManagerExecuteStreamTransparentWebsocketSkipsInvalidCandidatesUntilCodexWebsocketAuth(t *testing.T) {
	selector := &transparentWebsocketSelector{order: []string{"auth-openai", "auth-codex-ws"}}
	manager := NewManager(nil, selector, nil)

	openaiExec := &transparentWebsocketCaptureExecutor{provider: "openai"}
	codexExec := &transparentWebsocketCaptureExecutor{provider: "codex"}
	manager.RegisterExecutor(openaiExec)
	manager.RegisterExecutor(codexExec)

	authOpenAI := &Auth{ID: "auth-openai", Provider: "openai", Status: StatusActive}
	authCodexWS := &Auth{
		ID:         "auth-codex-ws",
		Provider:   "codex",
		Status:     StatusActive,
		Attributes: map[string]string{"websockets": "true"},
	}
	if _, err := manager.Register(context.Background(), authOpenAI); err != nil {
		t.Fatalf("register openai auth: %v", err)
	}
	if _, err := manager.Register(context.Background(), authCodexWS); err != nil {
		t.Fatalf("register codex websocket auth: %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(authOpenAI.ID, authOpenAI.Provider, []*registry.ModelInfo{{ID: "gpt-5"}})
	registry.GetGlobalRegistry().RegisterClient(authCodexWS.ID, authCodexWS.Provider, []*registry.ModelInfo{{ID: "gpt-5"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(authOpenAI.ID)
		registry.GetGlobalRegistry().UnregisterClient(authCodexWS.ID)
	})

	streamResult, err := manager.ExecuteStream(context.Background(), []string{"openai", "codex"}, cliproxyexecutor.Request{
		Model:   "gpt-5",
		Payload: []byte(`{"model":"gpt-5","input":[]}`),
	}, cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.TransparentWebsocketModeMetadataKey: true,
		},
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	if streamResult == nil {
		t.Fatal("streamResult = nil")
	}
	for range streamResult.Chunks {
	}
	if calls := len(openaiExec.AuthIDs()); calls != 0 {
		t.Fatalf("openai executor calls = %d, want 0", calls)
	}
	gotCodex := codexExec.AuthIDs()
	if len(gotCodex) != 1 || gotCodex[0] != "auth-codex-ws" {
		t.Fatalf("codex executor authIDs = %v, want [auth-codex-ws]", gotCodex)
	}
}
