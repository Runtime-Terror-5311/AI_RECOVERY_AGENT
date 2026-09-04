package razorpay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"revenue-recovery-agent/internal/events"
)

// WebhookPayload represents the outer structure received from Razorpay webhooks.
type WebhookPayload struct {
	Entity    string                 `json:"entity"`
	AccountID string                 `json:"account_id"`
	Event     string                 `json:"event"`
	Contains  []string               `json:"contains"`
	CreatedAt int64                  `json:"created_at"`
	Payload   WebhookInternalPayload `json:"payload"`
}

// WebhookInternalPayload contains the payment entity and order entity.
type WebhookInternalPayload struct {
	Payment struct {
		Entity PaymentEntity `json:"entity"`
	} `json:"payment"`
}

// PaymentEntity captures the payment fields from Razorpay.
type PaymentEntity struct {
	ID               string                 `json:"id"`
	Amount           int64                  `json:"amount"` // in paise (e.g. 50000 = ₹500.00)
	Currency         string                 `json:"currency"`
	Status           string                 `json:"status"`
	Order_ID         string                 `json:"order_id"`
	Method           string                 `json:"method"` // card, upi, netbanking, wallet
	Email            string                 `json:"email"`
	Contact          string                 `json:"contact"`
	ErrorCode        string                 `json:"error_code"`
	ErrorDescription string                 `json:"error_description"`
	ErrorSource      string                 `json:"error_source"`
	ErrorStep        string                 `json:"error_step"`
	ErrorReason      string                 `json:"error_reason"`
	CreatedAt        int64                  `json:"created_at"`
	Notes            map[string]interface{} `json:"notes"`
}

// VerifyWebhookSignature verifies the X-Razorpay-Signature header against the raw body using the secret.
func VerifyWebhookSignature(body []byte, signature string, secret string) bool {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	expectedSignature := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(expectedSignature), []byte(signature))
}

// ToRawPaymentEvent converts a Razorpay webhook into our canonical RawPaymentEvent.
func (wp *WebhookPayload) ToRawPaymentEvent() (events.RawPaymentEvent, error) {
	pe := wp.Payload.Payment.Entity
	if pe.ID == "" {
		return events.RawPaymentEvent{}, errors.New("invalid webhook payload: missing payment entity ID")
	}

	eventType := events.EventTypePaymentFailed
	if wp.Event == "payment.authorized" || wp.Event == "payment.captured" {
		eventType = events.EventTypePaymentSuccess
	}

	// Amount is converted from paise to rupees
	amountRupees := float64(pe.Amount) / 100.0

	// Extract or default merchant ID from AccountID or notes
	merchantID := wp.AccountID
	if merchantID == "" {
		merchantID = "razorpay_account_default"
	}

	ts := time.Unix(pe.CreatedAt, 0).UTC()
	if pe.CreatedAt == 0 {
		ts = time.Now().UTC()
	}

	raw := events.RawPaymentEvent{
		EventID:             fmt.Sprintf("rzp_evt_%s_%d", pe.ID, time.Now().UnixNano()),
		TransactionID:       pe.ID,
		MerchantID:          merchantID,
		EventType:           eventType,
		Amount:              amountRupees,
		Currency:            pe.Currency,
		PaymentMethod:       pe.Method,
		GatewayResponseCode: pe.ErrorCode,
		GatewayMessage:      fmt.Sprintf("%s (%s: %s)", pe.ErrorDescription, pe.ErrorSource, pe.ErrorReason),
		Timestamp:           ts,
		AttemptNumber:       1,
		CustomerID:          pe.Contact,
		Metadata: map[string]interface{}{
			"order_id":     pe.Order_ID,
			"error_source": pe.ErrorSource,
			"error_step":   pe.ErrorStep,
			"error_reason": pe.ErrorReason,
			"notes":        pe.Notes,
		},
	}

	return raw, nil
}

// ParseWebhook parses and validates raw webhook bytes.
func ParseWebhook(body []byte) (*WebhookPayload, error) {
	var wp WebhookPayload
	if err := json.Unmarshal(body, &wp); err != nil {
		return nil, err
	}
	return &wp, nil
}
