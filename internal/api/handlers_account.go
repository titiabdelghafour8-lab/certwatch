package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"github.com/titiabdelghafour8/certwatch/internal/model"
	"github.com/titiabdelghafour8/certwatch/internal/plans"
	"github.com/titiabdelghafour8/certwatch/internal/store"
)

func parseEmail(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Address != raw || len(raw) > 254 || !strings.Contains(raw[strings.LastIndex(raw, "@"):], ".") {
		return "", false
	}
	return strings.ToLower(raw), true
}

// handleSignup creates an account. When SMTP is configured the API key is
// emailed, which proves ownership of the address; otherwise (typical for
// self-hosting) it is returned directly.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.SignupEnabled {
		writeError(w, http.StatusForbidden, "signup_disabled", "signups are disabled on this server")
		return
	}
	if !s.signupLimiter.Allow("ip:" + s.clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many signup attempts, try again later")
		return
	}
	var req model.SignupRequest
	if !decode(w, r, &req) {
		return
	}
	email, ok := parseEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_email", "a valid email address is required")
		return
	}

	verify := s.notifier.EmailEnabled()
	sentMsg := "Check your inbox: we emailed your API key to " + email + ". Then run `certwatch login`."

	user, err := s.store.CreateUser(r.Context(), email, s.cfg.DefaultPlan)
	if errors.Is(err, store.ErrConflict) {
		if verify {
			// Same answer as a fresh signup so the endpoint does not reveal
			// which emails are registered. The owner can use /v1/recover.
			writeJSON(w, http.StatusAccepted, model.SignupResponse{Message: sentMsg})
			return
		}
		writeError(w, http.StatusConflict, "email_taken", "an account with this email already exists")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	secret, err := s.issueKey(r.Context(), user.ID, "default")
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("user signed up", "user_id", user.ID, "plan", user.Plan)

	if verify {
		if err := s.emailKey(r.Context(), email, secret, "Welcome to certwatch"); err != nil {
			s.log.Error("send signup email", "user_id", user.ID, "err", err)
			writeError(w, http.StatusBadGateway, "email_failed", "account created but the email could not be sent; use `certwatch recover` to retry")
			return
		}
		writeJSON(w, http.StatusAccepted, model.SignupResponse{Message: sentMsg})
		return
	}
	writeJSON(w, http.StatusCreated, model.SignupResponse{User: &user, APIKey: secret, Message: "Account created."})
}

// handleRecover emails a new API key to an existing account.
func (s *Server) handleRecover(w http.ResponseWriter, r *http.Request) {
	if !s.signupLimiter.Allow("ip:" + s.clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
		return
	}
	if !s.notifier.EmailEnabled() {
		writeError(w, http.StatusNotImplemented, "email_disabled", "key recovery needs email, which is not configured on this server")
		return
	}
	var req model.RecoverRequest
	if !decode(w, r, &req) {
		return
	}
	email, ok := parseEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_email", "a valid email address is required")
		return
	}
	msg := model.MessageResponse{Message: "If an account exists for " + email + ", a new API key is on its way."}
	user, err := s.store.GetUserByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusAccepted, msg)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	secret, err := s.issueKey(r.Context(), user.ID, "recovered")
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.emailKey(r.Context(), user.Email, secret, "Your new certwatch API key"); err != nil {
		s.log.Error("send recovery email", "user_id", user.ID, "err", err)
	}
	writeJSON(w, http.StatusAccepted, msg)
}

func (s *Server) issueKey(ctx context.Context, userID int64, name string) (string, error) {
	secret, prefix, hash, err := newAPIKey()
	if err != nil {
		return "", err
	}
	if _, err := s.store.CreateAPIKey(ctx, userID, name, prefix, hash); err != nil {
		return "", err
	}
	return secret, nil
}

func (s *Server) emailKey(ctx context.Context, to, secret, subject string) error {
	body := fmt.Sprintf(`Here is your certwatch API key:

    %s

Log in from your terminal:

    certwatch login --api-key %s

Keep this key secret. You can create and revoke keys with "certwatch keys".
If you did not request this, you can ignore this email.

--
certwatch · %s
`, secret, secret, s.cfg.PublicURL)
	return s.notifier.SendEmail(ctx, to, subject, body)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, u model.User) {
	monitors, err := s.store.CountMonitors(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	channels, err := s.store.CountChannels(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, model.MeResponse{
		User:           u,
		Plan:           plans.Get(u.Plan).Info(),
		Usage:          model.Usage{Monitors: monitors, Channels: channels},
		BillingEnabled: s.billing.Enabled(),
	})
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request, u model.User) {
	keys, err := s.store.ListAPIKeys(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, model.List[model.APIKey]{Items: keys})
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request, u model.User) {
	var req model.CreateKeyRequest
	if !decode(w, r, &req) {
		return
	}
	keys, err := s.store.ListAPIKeys(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if len(keys) >= 20 {
		writeError(w, http.StatusConflict, "too_many_keys", "revoke an existing key first (limit 20)")
		return
	}
	secret, prefix, hash, err := newAPIKey()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	key, err := s.store.CreateAPIKey(r.Context(), u.ID, clean(req.Name, 64), prefix, hash)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, model.CreateKeyResponse{Key: key, Secret: secret})
}

func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request, u model.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.store.RevokeAPIKey(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "key not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
