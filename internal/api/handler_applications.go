package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"fluxen/internal/store"
)

type createApplicationRequest struct {
	Name string `json:"name"`
}

type applicationResponse struct {
	ID          string     `json:"id"`
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	FirstSeenAt *time.Time `json:"first_seen_at,omitempty"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
}

func toApplicationResponse(a store.Application) applicationResponse {
	return applicationResponse{
		ID: string(a.ID), Slug: a.Slug, Name: a.Name, Status: a.Status,
		CreatedAt: a.CreatedAt, FirstSeenAt: a.FirstSeenAt, LastSeenAt: a.LastSeenAt,
	}
}

// handleCreateApplication creates an application, deriving its slug from
// the name (Part L Phase 1: "slug auto-generated, editable" — editing
// comes with the fuller Application Detail screen in Phase 2; Phase 1
// only needs a working, unique slug at creation time).
func (s *Server) handleCreateApplication(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)

	var req createApplicationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	slug, err := s.Apps.UniqueSlug(r.Context(), uc.OrgID, req.Name)
	if err != nil {
		s.Logger.Error("api: failed to generate slug", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	app, err := s.Apps.Create(r.Context(), uc.OrgID, slug, req.Name)
	if err != nil {
		s.Logger.Error("api: failed to create application", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, toApplicationResponse(app))
}

func (s *Server) handleListApplications(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)

	apps, err := s.Apps.ListByOrg(r.Context(), uc.OrgID)
	if err != nil {
		s.Logger.Error("api: failed to list applications", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]applicationResponse, 0, len(apps))
	for _, a := range apps {
		out = append(out, toApplicationResponse(a))
	}
	writeJSON(w, http.StatusOK, out)
}
