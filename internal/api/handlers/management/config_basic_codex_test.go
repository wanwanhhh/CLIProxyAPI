package management

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestPutCodexTransparentWebsocketMode_PersistsConfig(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	configPath := writeTestConfigFile(t)
	h := &Handler{
		cfg:            &config.Config{},
		configFilePath: configPath,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/codex/transparent-websocket-mode", bytes.NewBufferString(`{"value":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.PutCodexTransparentWebsocketMode(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !h.cfg.Codex.TransparentWebsocketMode {
		t.Fatalf("handler cfg TransparentWebsocketMode = false, want true")
	}

	reloaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !reloaded.Codex.TransparentWebsocketMode {
		t.Fatalf("reloaded cfg TransparentWebsocketMode = false, want true")
	}
}

func TestGetCodexTransparentWebsocketMode_ReturnsCurrentValue(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	h := &Handler{
		cfg: &config.Config{
			SDKConfig: config.SDKConfig{
				Codex: config.CodexConfig{
					TransparentWebsocketMode: true,
				},
			},
		},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/codex/transparent-websocket-mode", nil)

	h.GetCodexTransparentWebsocketMode(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "{\"transparent-websocket-mode\":true}" {
		t.Fatalf("body = %s, want %s", got, "{\"transparent-websocket-mode\":true}")
	}
}
