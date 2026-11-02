package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/titiabdelghafour8/certwatch/internal/model"
	"github.com/titiabdelghafour8/certwatch/internal/notify"
	"github.com/titiabdelghafour8/certwatch/internal/plans"
	"github.com/titiabdelghafour8/certwatch/internal/store"
)

// validateChannelTarget checks that a channel target is well formed and safe
// to deliver to.
func (s *Server) validateChannelTarget(u model.User, kind model.ChannelKind, target string) (string, error) {
	target = strings.TrimSpace(target)
	if kind == model.ChannelEmail {
		email, ok := parseEmail(target)
		if !ok {
			return "", errors.New("target must be a valid email address")
		}
		// Prevents using the service to send mail to arbitrary people.
		if s.cfg.RestrictEmailChannels && !strings.EqualFold(email, u.Email) {
			return "", errors.New("email channels can only target your account email; use a webhook for other recipients")
		}
		if !s.notifier.EmailEnabled() {
			return "", errors.New("email delivery is not configured on this server")
		}
		return email, nil
	}

	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" {
		return "", errors.New("target must be an absolute URL")
	}
	if len(target) > 2048 {
		return "", errors.New("URL is too long")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && s.cfg.AllowPrivateTargets) {
		return "", errors.New("webhook URLs must use https")
	}
	host := strings.ToLower(parsed.Hostname())
	switch kind {
	case model.ChannelSlack:
		if host != "hooks.slack.com" {
			return "", errors.New("slack target must be an incoming webhook URL (https://hooks.slack.com/...)")
		}
	case model.ChannelDiscord:
		if (host != "discord.com" && host != "discordapp.com") || !strings.HasPrefix(parsed.Path, "/api/webhooks/") {
			return "", errors.New("discord target must be a webhook URL (https://discord.com/api/webhooks/...)")
		}
	}
	return target, nil
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request, u model.User) {
	chs, err := s.store.ListChannels(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	for i := range chs {
		chs[i] = chs[i].Masked()
	}
	writeJSON(w, http.StatusOK, model.List[model.Channel]{Items: chs})
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request, u model.User) {
	var req model.CreateChannelRequest
	if !decode(w, r, &req) {
		return
	}
	if !req.Kind.Valid() {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind must be one of email, slack, discord, webhook")
		return
	}
	target, err := s.validateChannelTarget(u, req.Kind, req.Target)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}
	plan := plans.Get(u.Plan)
	count, err := s.store.CountChannels(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !plans.Allows(plan.MaxChannels, count) {
		writeError(w, http.StatusPaymentRequired, "plan_limit",
			fmt.Sprintf("the %s plan allows %d notification channels; run `certwatch upgrade` for more", plan.Name, plan.MaxChannels))
		return
	}
	ch, err := s.store.CreateChannel(r.Context(), model.Channel{UserID: u.ID, Kind: req.Kind, Target: target, Name: clean(req.Name, 64)})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, ch.Masked())
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request, u model.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.store.DeleteChannel(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "channel not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestChannel(w http.ResponseWriter, r *http.Request, u model.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ch, err := s.store.GetChannel(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "channel not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.notifier.Send(r.Context(), ch, notify.TestMessage()); err != nil {
		writeError(w, http.StatusBadGateway, "delivery_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.MessageResponse{Message: "test notification delivered"})
}
