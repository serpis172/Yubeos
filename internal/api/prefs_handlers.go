package api

import (
	"errors"
	"net/http"

	"github.com/serpis172/Yubeos/internal/prefs"
	"github.com/serpis172/Yubeos/internal/problem"
)

func (s *Server) registerPrefsRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/me/preferences", s.require(RoleUser, s.getPrefs))
	mux.Handle("PUT /api/v1/me/preferences", s.require(RoleUser, s.putPrefs))
}

func (s *Server) prefsStore() prefs.Store {
	if s.opts.Prefs != nil {
		return s.opts.Prefs
	}
	return s.memPrefs // demo / tests without a database
}

func (s *Server) getPrefs(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	out, err := s.prefsStore().Get(r.Context(), p.Username)
	if err != nil {
		s.opts.Log.Error("prefs get", "err", err)
		problem.Internal(w, r)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putPrefs(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, prefs.MaxBytes+4096)
	var in prefs.Prefs
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.prefsStore().Put(r.Context(), p.Username, in)
	switch {
	case errors.Is(err, prefs.ErrInvalid):
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation", "Invalid preferences", err.Error())
	case err != nil:
		s.opts.Log.Error("prefs put", "err", err)
		problem.Internal(w, r)
	default:
		writeJSON(w, http.StatusOK, out)
	}
}
