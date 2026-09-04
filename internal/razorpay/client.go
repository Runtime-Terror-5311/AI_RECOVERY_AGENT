package razorpay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client interacts with the Razorpay REST API.
type Client struct {
	KeyID      string
	KeySecret  string
	BaseURL    string
	httpClient *http.Client
}

// NewClient initializes a Razorpay client with authentication credentials.
func NewClient(keyID, keySecret string) *Client {
	return &Client{
		KeyID:      keyID,
		KeySecret:  keySecret,
		BaseURL:    "https://api.razorpay.com/v1",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// PaymentLinkRequest captures parameters to create a recovery payment link.
type PaymentLinkRequest struct {
	Amount      int64  `json:"amount"` // paise
	Currency    string `json:"currency"`
	Description string `json:"description"`
	Customer    struct {
		Name    string `json:"name,omitempty"`
		Contact string `json:"contact,omitempty"`
		Email   string `json:"email,omitempty"`
	} `json:"customer"`
	Notify struct {
		SMS   bool `json:"sms"`
		Email bool `json:"email"`
	} `json:"notify"`
	ReminderEnable bool `json:"reminder_enable"`
}

// PaymentLinkResponse captures the response from Razorpay.
type PaymentLinkResponse struct {
	ID        string `json:"id"`
	ShortURL  string `json:"short_url"`
	Status    string `json:"status"`
	Amount    int64  `json:"amount"`
	CreatedAt int64  `json:"created_at"`
}

// CreatePaymentLink generates a dynamic Razorpay payment link for customer re-attempts.
func (c *Client) CreatePaymentLink(ctx context.Context, req PaymentLinkRequest) (*PaymentLinkResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/payment_links", c.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}

	c.setAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("razorpay API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var res PaymentLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	return &res, nil
}

func (c *Client) setAuth(req *http.Request) {
	auth := fmt.Sprintf("%s:%s", c.KeyID, c.KeySecret)
	encoded := base64.StdEncoding.EncodeToString([]byte(auth))
	req.Header.Set("Authorization", "Basic "+encoded)
}
