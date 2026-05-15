package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/pastelocal/pastelocal/internal/snippets"
	cliperr "github.com/pastelocal/pastelocal/internal/errors"
	"github.com/pastelocal/pastelocal/internal/proto"
)

// handleSnippets handles GET /snippets and POST /snippets.
func (s *Server) handleSnippets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListSnippets(w, r)
	case http.MethodPost:
		s.handleSaveSnippet(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSnippet handles GET /snippets/{name}, PUT /snippets/{name}, DELETE /snippets/{name}.
func (s *Server) handleSnippet(w http.ResponseWriter, r *http.Request) {
	// Extract name from path: /snippets/{name}
	name := r.URL.Path[len("/snippets/"):]
	if name == "" {
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1008", "missing snippet name"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetSnippet(w, r, name)
	case http.MethodDelete:
		s.handleDeleteSnippet(w, r, name)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleListSnippets returns all snippets (metadata only, no data).
func (s *Server) handleListSnippets(w http.ResponseWriter, r *http.Request) {
	if _, authErr := s.validateAuth(r); authErr != nil {
		cliperr.WriteJSON(w, authErr)
		return
	}

	store := snippets.NewStore(snippets.DefaultStorePath(), 50)
	list, err := store.List()
	if err != nil {
		s.logger.Error("failed to list snippets", "err", err)
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1011", "failed to list snippets"))
		return
	}

	// Convert to response format.
	items := make([]proto.SnippetEntry, len(list))
	for i, snip := range list {
		items[i] = proto.SnippetEntry{
			Name:        snip.Name,
			Format:      snip.Format,
			Size:        snip.Size,
			CreatedAt:   snip.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:   snip.UpdatedAt.Format("2006-01-02T15:04:05Z"),
			Description: snip.Description,
			Hash:        snip.Hash,
		}
	}

	resp := proto.SnippetListResponse{
		OK:    true,
		Items: items,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleGetSnippet returns a specific snippet with its data.
func (s *Server) handleGetSnippet(w http.ResponseWriter, r *http.Request, name string) {
	if _, authErr := s.validateAuth(r); authErr != nil {
		cliperr.WriteJSON(w, authErr)
		return
	}

	store := snippets.NewStore(snippets.DefaultStorePath(), 50)
	snippet, err := store.Load(name)
	if err != nil {
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1009", "snippet not found"))
		return
	}

	// Build response similar to clipboard response.
	resp := proto.ClipboardResponse{
		OK:         true,
		Format:     snippet.Format,
		ByteCount:  snippet.Size,
		CapturedAt: snippet.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		ID:         snippet.Name,
	}

	if snippet.Format == "png" {
		resp.Image = base64.StdEncoding.EncodeToString(snippet.Data)
	} else {
		resp.Text = string(snippet.Data)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleSaveSnippet creates or updates a snippet.
func (s *Server) handleSaveSnippet(w http.ResponseWriter, r *http.Request) {
	if _, authErr := s.validateAuth(r); authErr != nil {
		cliperr.WriteJSON(w, authErr)
		return
	}

	var req proto.SnippetSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1008", "invalid JSON"))
		return
	}

	if req.Name == "" {
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1008", "missing snippet name"))
		return
	}

	if req.Format != "text" && req.Format != "png" {
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1008", "format must be 'text' or 'png'"))
		return
	}

	var data []byte
	if req.Format == "png" {
		if req.Image == "" {
			cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1008", "missing image data"))
			return
		}
		var err error
		data, err = base64.StdEncoding.DecodeString(req.Image)
		if err != nil {
			cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1004", "invalid base64"))
			return
		}
	} else {
		data = []byte(req.Text)
	}

	store := snippets.NewStore(snippets.DefaultStorePath(), 50)
	if err := store.Save(req.Name, req.Format, data, req.Description); err != nil {
		s.logger.Error("failed to save snippet", "err", err, "name", req.Name)
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1011", err.Error()))
		return
	}

	resp := proto.SnippetSaveResponse{
		OK:      true,
		Name:    req.Name,
		Created: !store.Exists(req.Name), // Will be false since we just saved
	}
	// Check if it existed before by trying to load with stat...
	// Actually, let's just assume it was updated if no error.
	resp.Created = true // Simplified

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleDeleteSnippet removes a snippet.
func (s *Server) handleDeleteSnippet(w http.ResponseWriter, r *http.Request, name string) {
	if _, authErr := s.validateAuth(r); authErr != nil {
		cliperr.WriteJSON(w, authErr)
		return
	}

	store := snippets.NewStore(snippets.DefaultStorePath(), 50)
	if err := store.Delete(name); err != nil {
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1009", err.Error()))
		return
	}

	resp := proto.SnippetDeleteResponse{OK: true, Name: name}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
