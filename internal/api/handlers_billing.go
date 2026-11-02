package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/titiabdelghafour8-lab/certwatch/internal/billing"
	"github.com/titiabdelghafour8-lab/certwatch/internal/model"
	"github.com/titiabdelghafour8-lab/certwatch/internal/plans"
	"github.com/titiabdelghafour8-lab/certwatch/internal/store"
)

func (s *Server) billingDisabled(w http.ResponseWriter) bool {
	if !s.billing.Enabled() {
		writeError(w, http.StatusNotImplemented, "billing_disabled", "billing is not enabled on this server")
		return true
	}
	return false
}

func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request, u model.User) {
	if s.billingDisabled(w) {
		return
	}
	var req model.CheckoutRequest
	if !decode(w, r, &req) {
		return
	}
	if _, ok := s.billing.PriceFor(req.Plan); !ok {
		writeError(w, http.StatusBadRequest, "invalid_plan", "unknown or unavailable plan")
		return
	}
	if u.Plan == req.Plan {
		writeError(w, http.StatusConflict, "already_subscribed", "you are already on this plan")
		return
	}
	// Existing subscribers change plans in the portal so Stripe prorates.
	if u.StripeSubscriptionID != "" {
		url, err := s.billing.CreatePortal(r.Context(), u.StripeCustomerID)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, model.URLResponse{URL: url})
		return
	}
	url, err := s.billing.CreateCheckout(r.Context(), u.ID, u.Email, u.StripeCustomerID, req.Plan)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, model.URLResponse{URL: url})
}

func (s *Server) handlePortal(w http.ResponseWriter, r *http.Request, u model.User) {
	if s.billingDisabled(w) {
		return
	}
	if u.StripeCustomerID == "" {
		writeError(w, http.StatusConflict, "no_subscription", "you have no subscription yet; run `certwatch upgrade`")
		return
	}
	url, err := s.billing.CreatePortal(r.Context(), u.StripeCustomerID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, model.URLResponse{URL: url})
}

// handleStripeWebhook keeps users' plans in sync with their subscriptions.
// Handlers are idempotent because Stripe may deliver an event more than once.
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if s.billingDisabled(w) {
		return
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read body")
		return
	}
	ev, err := s.billing.ParseWebhook(payload, r.Header.Get("Stripe-Signature"), s.now())
	if err != nil {
		s.log.Warn("rejected stripe webhook", "err", err)
		writeError(w, http.StatusBadRequest, "invalid_signature", "invalid signature")
		return
	}
	log := s.log.With("stripe_event", ev.ID, "type", ev.Type)

	switch ev.Type {
	case "checkout.session.completed":
		var cs billing.CheckoutSession
		if err := json.Unmarshal(ev.Data.Object, &cs); err != nil {
			writeError(w, http.StatusBadRequest, "bad_event", err.Error())
			return
		}
		userID, err := strconv.ParseInt(cs.ClientReferenceID, 10, 64)
		plan := cs.Metadata["plan"]
		if err != nil || !plans.Exists(plan) {
			log.Warn("checkout session without usable reference", "ref", cs.ClientReferenceID, "plan", plan)
			break
		}
		if err := s.store.SetBilling(r.Context(), userID, plan, cs.Customer, cs.Subscription); err != nil && !errors.Is(err, store.ErrNotFound) {
			s.internalError(w, r, err)
			return
		}
		log.Info("subscription started", "user_id", userID, "plan", plan)

	case "customer.subscription.updated", "customer.subscription.deleted":
		var sub billing.Subscription
		if err := json.Unmarshal(ev.Data.Object, &sub); err != nil {
			writeError(w, http.StatusBadRequest, "bad_event", err.Error())
			return
		}
		u, err := s.store.GetUserByStripeCustomer(r.Context(), sub.Customer)
		if errors.Is(err, store.ErrNotFound) {
			log.Warn("subscription event for unknown customer", "customer", sub.Customer)
			break
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		plan, subID := plans.Free.Name, ""
		if ev.Type == "customer.subscription.updated" && sub.Active() {
			if p, ok := s.billing.PlanForPrice(sub.PriceID()); ok {
				plan, subID = p, sub.ID
			}
		}
		if err := s.store.SetBilling(r.Context(), u.ID, plan, sub.Customer, subID); err != nil {
			s.internalError(w, r, err)
			return
		}
		log.Info("subscription changed", "user_id", u.ID, "plan", plan, "status", sub.Status)

	default:
		log.Debug("ignored stripe event")
	}
	writeJSON(w, http.StatusOK, map[string]bool{"received": true})
}
