package events

import (
	"context"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
)

// NewStripeClient returns the real SDK-backed client, or the unconfigured
// stub when no secret key is set (dev without payments, free events only).
func NewStripeClient(secretKey string) StripeClient {
	if secretKey == "" {
		return unconfiguredStripe{}
	}
	return &stripeClient{sc: stripe.NewClient(secretKey)}
}

type stripeClient struct{ sc *stripe.Client }

func (c *stripeClient) Configured() bool { return true }

func (c *stripeClient) CreateCheckoutSession(ctx context.Context, p CheckoutParams) (*CheckoutSession, error) {
	params := &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String(stripe.CheckoutSessionModePayment),
		SuccessURL:        stripe.String(p.SuccessURL),
		CancelURL:         stripe.String(p.CancelURL),
		ClientReferenceID: stripe.String(p.RegistrationID.String()),
		ExpiresAt:         stripe.Int64(p.ExpiresAt.Unix()),
		Metadata:          map[string]string{"registration_id": p.RegistrationID.String()},
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   stripe.String(p.Currency),
				UnitAmount: stripe.Int64(p.AmountCents),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name: stripe.String(p.EventName),
				},
			},
		}},
	}
	sess, err := c.sc.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return nil, err
	}
	return &CheckoutSession{ID: sess.ID, URL: sess.URL, ExpiresAt: time.Unix(sess.ExpiresAt, 0)}, nil
}

func (c *stripeClient) RefundPaymentIntent(ctx context.Context, paymentIntentID string) error {
	_, err := c.sc.V1Refunds.Create(ctx, &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(paymentIntentID),
	})
	return err
}
