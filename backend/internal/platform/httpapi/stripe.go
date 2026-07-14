package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	stripe "github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"

	"github.com/mjabloniec/cube-planner/backend/internal/events"
)

// stripeWebhookHandler verifies the Stripe-Signature header against the
// raw body (which is why this lives outside huma) and forwards a minimal
// event shape to the service. 200 only after the DB transaction commits;
// 5xx makes Stripe retry.
func stripeWebhookHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "body too large", http.StatusBadRequest)
			return
		}
		ev, err := webhook.ConstructEventWithOptions(payload,
			r.Header.Get("Stripe-Signature"), deps.StripeWebhookSecret,
			webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
		if err != nil {
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}
		we := events.WebhookEvent{ID: ev.ID, Type: string(ev.Type)}
		switch ev.Type {
		case "checkout.session.completed", "checkout.session.expired":
			var s stripe.CheckoutSession
			if err := json.Unmarshal(ev.Data.Raw, &s); err != nil {
				http.Error(w, "bad payload", http.StatusBadRequest)
				return
			}
			we.CheckoutSessionID = s.ID
			if s.PaymentIntent != nil {
				we.PaymentIntentID = s.PaymentIntent.ID
			}
		case "charge.refunded":
			var c stripe.Charge
			if err := json.Unmarshal(ev.Data.Raw, &c); err != nil {
				http.Error(w, "bad payload", http.StatusBadRequest)
				return
			}
			if c.PaymentIntent != nil {
				we.PaymentIntentID = c.PaymentIntent.ID
			}
		}
		if err := deps.Events.HandleWebhookEvent(r.Context(), we); err != nil {
			slog.Error("stripe webhook processing failed", "event", ev.ID, "type", ev.Type, "error", err)
			http.Error(w, "processing failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
