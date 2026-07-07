package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dcode/internal/config"
	"dcode/internal/core"
	"dcode/internal/tutor"
	"dcode/internal/vault"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	code, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	notes, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Server{svc: core.New(code, notes, tutor.New(config.AIConfig{Provider: "openai"}))}
}

func TestSaveGetAndTreeEndpoints(t *testing.T) {
	s := testServer(t)
	h := s.routes()

	saveReq := httptest.NewRequest(http.MethodPut, "/api/note", strings.NewReader(`{"path":"Notes.md","body":"# Notes\n\nhello"}`))
	saveReq.Header.Set("Content-Type", "application/json")
	saveW := httptest.NewRecorder()
	h.ServeHTTP(saveW, saveReq)
	if saveW.Code != http.StatusOK {
		t.Fatalf("save status = %d; body=%q", saveW.Code, saveW.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/note?path=Notes.md", nil)
	getW := httptest.NewRecorder()
	h.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("get status = %d; body=%q", getW.Code, getW.Body.String())
	}
	var got struct {
		Path string `json:"path"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != "Notes.md" || !strings.Contains(got.Body, "hello") {
		t.Fatalf("got note %+v", got)
	}

	treeReq := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
	treeW := httptest.NewRecorder()
	h.ServeHTTP(treeW, treeReq)
	if treeW.Code != http.StatusOK {
		t.Fatalf("tree status = %d; body=%q", treeW.Code, treeW.Body.String())
	}
	if !strings.Contains(treeW.Body.String(), "Notes.md") {
		t.Fatalf("tree should include saved note, got %q", treeW.Body.String())
	}
}

func TestGenerateEndpointReturnsGone(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(`{"request":"http"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.routes().ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("status = %d, want %d; body=%q", w.Code, http.StatusGone, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "removed") {
		t.Fatalf("body should explain removal, got %q", w.Body.String())
	}
}
