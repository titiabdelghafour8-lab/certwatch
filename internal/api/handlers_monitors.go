package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/titiabdelghafour8/certwatch/internal/alerting"
	"github.com/titiabdelghafour8/certwatch/internal/checker"
	"github.com/titiabdelghafour8/certwatch/internal/model"
	"github.com/titiabdelghafour8/certwatch/internal/plans"
	"github.com/titiabdelghafour8/certwatch/internal/store"
)

func (s *Server) handleListMonitors(w http.ResponseWriter, r *http.Request, u model.User) {
	ms, err := s.store.ListMonitors(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	now := s.now()
	for i := range ms {
		ms[i].FillDaysLeft(now)
	}
	writeJSON(w, http.StatusOK, model.List[model.Monitor]{Items: ms})
}

func (s *Server) handleCreateMonitor(w http.ResponseWriter, r *http.Request, u model.User) {
	var req model.CreateMonitorRequest
	if !decode(w, r, &req) {
		return
	}
	if !req.Kind.Valid() {
		writeError(w, http.StatusBadRequest, "invalid_kind", `kind must be "tls" or "domain"`)
		return
	}
	var target string
	var err error
	if req.Kind == model.KindTLS {
		target, err = checker.NormalizeTLSTarget(req.Target, 443)
	} else {
		target, err = checker.NormalizeDomain(req.Target)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}
	warn, err := alerting.NormalizeWarnDays(req.WarnDays)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_warn_days", err.Error())
		return
	}

	plan := plans.Get(u.Plan)
	count, err := s.store.CountMonitors(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !plans.Allows(plan.MaxMonitors, count) {
		writeError(w, http.StatusPaymentRequired, "plan_limit",
			fmt.Sprintf("the %s plan allows %d monitors; run `certwatch upgrade` for more", plan.Name, plan.MaxMonitors))
		return
	}

	m, err := s.store.CreateMonitor(r.Context(), model.Monitor{
		UserID: u.ID, Kind: req.Kind, Target: target, Label: clean(req.Label, 100), WarnDays: warn,
	})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "already_exists", fmt.Sprintf("a %s monitor for %s already exists", req.Kind, target))
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) loadMonitor(w http.ResponseWriter, r *http.Request, u model.User) (model.Monitor, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return model.Monitor{}, false
	}
	m, err := s.store.GetMonitor(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "monitor not found")
		return m, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return m, false
	}
	return m, true
}

func (s *Server) handleGetMonitor(w http.ResponseWriter, r *http.Request, u model.User) {
	m, ok := s.loadMonitor(w, r, u)
	if !ok {
		return
	}
	m.FillDaysLeft(s.now())
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleDeleteMonitor(w http.ResponseWriter, r *http.Request, u model.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.store.DeleteMonitor(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "monitor not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCheckMonitor runs a check synchronously so the CLI can show results
// right after "certwatch add".
func (s *Server) handleCheckMonitor(w http.ResponseWriter, r *http.Request, u model.User) {
	m, ok := s.loadMonitor(w, r, u)
	if !ok {
		return
	}
	m = s.checker.Process(r.Context(), m)
	m.FillDaysLeft(s.now())
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handlePauseMonitor(paused bool) authedHandler {
	return func(w http.ResponseWriter, r *http.Request, u model.User) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		m, err := s.store.SetMonitorPaused(r.Context(), u.ID, id, paused)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "monitor not found")
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		m.FillDaysLeft(s.now())
		writeJSON(w, http.StatusOK, m)
	}
}
