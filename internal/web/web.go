// Package web is d-code's local browser front-end: a small net/http server that
// serves a 3-pane single-page UI (file tree | editor+preview | AI chat) over
// the core engine. It is started with `dcode serve`.
//
// Architecture note: handlers here are deliberately thin — parse the request,
// call a core.Service method, render JSON or HTML. All business logic lives in
// internal/core so the TUI and this UI stay in feature parity. The one thing
// web owns is presentation: turning a note's markdown into HTML for preview.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"dcode/internal/core"
	"dcode/internal/tutor"
)

//go:embed index.html
var assets embed.FS

var wikilinkHTML = regexp.MustCompile(`\[\[([^\]\[]+?)\]\]`)

func htmlAttr(s string) string { return html.EscapeString(s) }
func htmlText(s string) string { return html.EscapeString(s) }

// Server holds the engine the HTTP handlers drive, plus the markdown renderer
// (presentation, which is web's own concern).
type Server struct {
	svc *core.Service
	md  goldmark.Markdown
}

// Serve starts the web UI on addr (e.g. ":8765"), blocking until it stops.
func Serve(addr string, svc *core.Service) error {
	s := &Server{
		svc: svc,
		md:  goldmark.New(goldmark.WithExtensions(extension.GFM)),
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /api/tree", s.handleTree)
	mux.HandleFunc("GET /api/note", s.handleGetNote)
	mux.HandleFunc("PUT /api/note", s.handleSaveNote)
	mux.HandleFunc("POST /api/preview", s.handlePreview)
	mux.HandleFunc("POST /api/generate", s.handleGenerateRemoved)
	mux.HandleFunc("GET /api/backlinks", s.handleBacklinks)
	mux.HandleFunc("POST /api/chat", s.handleChat)
	return mux
}

// --- handlers (thin: parse -> core -> render) ---

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	page, err := assets.ReadFile("index.html")
	if err != nil {
		httpErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	notes, err := s.svc.ListNotes()
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"notes": notes})
}

func (s *Server) handleGetNote(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	n, err := s.svc.OpenNote(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, n)
}

func (s *Server) handleSaveNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Body string `json:"body"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Path == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	meta, err := s.svc.SaveNote(req.Path, req.Body)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, meta)
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var buf strings.Builder
	if err := s.md.Convert([]byte(req.Body), &buf); err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"html": renderWikilinks(buf.String())})
}

func (s *Server) handleGenerateRemoved(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "note generation was removed; use the editor to create notes and /api/chat or :explain for code help", http.StatusGone)
}

func (s *Server) handleBacklinks(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	back, err := s.svc.Backlinks(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"backlinks": back})
}

// handleChat streams the assistant's reply as plain text chunks. The request may
// name the open note (path) so the reply is grounded in what the user is
// currently reading.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		History []tutor.ChatTurn `json:"history"`
		Path    string           `json:"path"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	ctxText := ""
	if req.Path != "" {
		if n, err := s.svc.OpenNote(req.Path); err == nil {
			ctxText = "Current note — " + n.Title + "\n\nNote content:\n" + n.Body
		}
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	fl, _ := w.(http.Flusher)
	wrote := false
	_, err := s.svc.ChatStream(r.Context(), ctxText, req.History, func(d string) {
		_, _ = io.WriteString(w, d)
		wrote = true
		if fl != nil {
			fl.Flush()
		}
	})
	if err != nil {
		if !wrote {
			httpErr(w, err)
			return
		}
		_, _ = io.WriteString(w, "\n⚠ "+err.Error())
	}
}

// --- presentation helpers (web-owned) ---

// renderWikilinks turns [[Target]] / [[Target|alias]] left in rendered HTML into
// clickable spans the front-end intercepts. goldmark escapes the brackets as
// text, so this post-pass is safe to run on its output.
func renderWikilinks(htmlStr string) string {
	return wikilinkHTML.ReplaceAllStringFunc(htmlStr, func(m string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(m, "[["), "]]")
		target, alias := inner, inner
		if i := strings.IndexByte(inner, '|'); i >= 0 {
			target = strings.TrimSpace(inner[:i])
			alias = strings.TrimSpace(inner[i+1:])
		}
		return fmt.Sprintf(`<a href="#" class="wikilink" data-target="%s">%s</a>`,
			htmlAttr(target), htmlText(alias))
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func httpErr(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
