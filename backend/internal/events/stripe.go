package events

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrPaymentsUnconfigured: Stripe keys are absent. Creating/publishing a
// PAID event and paying map this to 503 payments-unconfigured; free
// events never hit it.
var ErrPaymentsUnconfigured = errors.New("payments not configured")

type CheckoutParams struct {
	RegistrationID uuid.UUID
	EventName      string
	Currency       string
	AmountCents    int64
	ExpiresAt      time.Time
	SuccessURL     string
	CancelURL      string
}

type CheckoutSession struct {
	ID        string
	URL       string
	ExpiresAt time.Time
}

// StripeClient isolates the SDK so the state machine tests never talk to
// real Stripe. The real implementation lands in stripe_client.go (Task 5).
type StripeClient interface {
	Configured() bool
	CreateCheckoutSession(ctx context.Context, p CheckoutParams) (*CheckoutSession, error)
	RefundPaymentIntent(ctx context.Context, paymentIntentID string) error
}

type unconfiguredStripe struct{}

func (unconfiguredStripe) Configured() bool { return false }

func (unconfiguredStripe) CreateCheckoutSession(context.Context, CheckoutParams) (*CheckoutSession, error) {
	return nil, ErrPaymentsUnconfigured
}

func (unconfiguredStripe) RefundPaymentIntent(context.Context, string) error {
	return ErrPaymentsUnconfigured
}
