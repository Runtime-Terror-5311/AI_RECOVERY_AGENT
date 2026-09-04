package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"revenue-recovery-agent/internal/events"
)

type causeProfile struct {
	trueCause      string
	recoverable    bool
	baseProb       float64
	codes          []string
	messages       []string
	paymentMethods []string
}

func main() {
	count := flag.Int("count", 100, "Number of payment events to generate")
	noiseRate := flag.Float64("noise-rate", 0.15, "Fraction of records with ambiguous/noisy signals (0.0 to 1.0)")
	outEvents := flag.String("out-events", "data/synthetic/batch-01.json", "Output file for raw events")
	outGroundTruth := flag.String("out-ground-truth", "data/ground-truth/batch-01.json", "Output file for ground truth")
	flag.Parse()

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	profiles := []causeProfile{
		{
			trueCause:   events.RootCauseGatewayTimeout,
			recoverable: true,
			baseProb:    0.78,
			codes:       []string{"GATEWAY_TIMEOUT", "TIMED_OUT", "REQ_TIMEOUT", "ERR_504"},
			messages: []string{
				"Transaction timed out waiting for issuer bank response",
				"Gateway SLA limit exceeded (5000ms)",
				"Upstream bank service latency high, dropped connection",
				"Timeout during 3DS OTP verification handshake",
			},
			paymentMethods: []string{events.PaymentMethodUPI, events.PaymentMethodNetbanking, events.PaymentMethodCard},
		},
		{
			trueCause:   events.RootCauseNetworkError,
			recoverable: true,
			baseProb:    0.74,
			codes:       []string{"CONN_RESET", "NETWORK_ERR", "ECONNRESET", "SOCKET_CLOSED"},
			messages: []string{
				"TCP connection reset by peer before ACK",
				"Socket connection closed abruptly during auth",
				"Network transport failure communicating with acquirer",
				"Broken pipe while streaming payment packet",
			},
			paymentMethods: []string{events.PaymentMethodCard, events.PaymentMethodUPI},
		},
		{
			trueCause:   events.RootCauseIssuerDecline,
			recoverable: true,
			baseProb:    0.65,
			codes:       []string{"DO_NOT_HONOR", "ISSUER_DECLINE", "SOFT_DECLINE", "CARD_DECLINED_05"},
			messages: []string{
				"Issuer bank declined transaction due to temporary policy",
				"Transaction declined (Do Not Honor code 05)",
				"Bank soft decline; retry permitted after cooldown",
				"Customer spending limit temporarily exceeded for the day",
			},
			paymentMethods: []string{events.PaymentMethodCard, events.PaymentMethodEmandate},
		},
		{
			trueCause:   events.RootCauseInsufficientFunds,
			recoverable: true,
			baseProb:    0.60,
			codes:       []string{"NSF", "INSUFFICIENT_FUNDS", "LOW_BALANCE", "BAL_ERR_51"},
			messages: []string{
				"Insufficient funds in customer source account",
				"Available balance is below requested transaction amount",
				"Account balance NSF (decline code 51)",
				"Debit failed due to lack of funds in primary wallet/account",
			},
			paymentMethods: []string{events.PaymentMethodNetbanking, events.PaymentMethodUPI, events.PaymentMethodEmandate},
		},
		{
			trueCause:   events.RootCauseExpiredCard,
			recoverable: false,
			baseProb:    0.00,
			codes:       []string{"EXPIRED_CARD", "CARD_EXPIRED", "INVALID_EXPIRY"},
			messages: []string{
				"Customer card has passed its expiration validity date",
				"Card expired; please update billing card details",
				"Payment method expired on file",
			},
			paymentMethods: []string{events.PaymentMethodCard, events.PaymentMethodEmandate},
		},
		{
			trueCause:   events.RootCauseRiskBlock,
			recoverable: false,
			baseProb:    0.00,
			codes:       []string{"RISK_FRAUD", "FRAUD_SUSPECTED", "VELOCITY_BLOCK", "BLACKLIST_BLOCK"},
			messages: []string{
				"Transaction blocked by risk engine due to high fraud score",
				"Suspicious velocity detected from IP and device fingerprint",
				"Card/VPA flagged on merchant global risk blacklist",
			},
			paymentMethods: []string{events.PaymentMethodUPI, events.PaymentMethodCard},
		},
		{
			trueCause:   events.RootCauseInvalidCredentials,
			recoverable: false,
			baseProb:    0.00,
			codes:       []string{"AUTH_FAIL", "BAD_OTP", "INVALID_PIN", "MPIN_MISMATCH"},
			messages: []string{
				"Customer entered incorrect 3DS OTP password",
				"Authentication failed: UPI MPIN verification mismatch",
				"Invalid security credentials or CVV provided by customer",
			},
			paymentMethods: []string{events.PaymentMethodUPI, events.PaymentMethodNetbanking},
		},
	}

	merchants := []struct {
		id          string
		health      string
		failPercent float64
	}{
		{"merch_flipkart_retail", "healthy", 0.04},
		{"merch_zomato_food", "healthy", 0.05},
		{"merch_swiggy_delivery", "healthy", 0.03},
		{"merch_glitchy_electronics", "degraded", 0.65}, // Unhealthy merchant to trigger circuit breaker!
	}

	now := time.Now().UTC()
	var rawEvents []events.RawPaymentEvent
	var groundTruths []events.GroundTruth

	for i := 1; i <= *count; i++ {
		txID := fmt.Sprintf("txn_%05d", i)
		evtID := fmt.Sprintf("evt_%05d_%d", i, r.Intn(90000)+10000)

		// Choose merchant based on distribution
		merch := merchants[r.Intn(len(merchants))]

		// Choose latent cause profile
		prof := profiles[r.Intn(len(profiles))]
		isNoisy := r.Float64() < *noiseRate

		// Pick code and message
		code := prof.codes[r.Intn(len(prof.codes))]
		msg := prof.messages[r.Intn(len(prof.messages))]
		method := prof.paymentMethods[r.Intn(len(prof.paymentMethods))]

		// Inject noise / ambiguity into observed fields if noisy
		if isNoisy {
			noiseType := r.Intn(3)
			if noiseType == 0 {
				code = "GENERIC_SYSTEM_ERR_99"
				msg = "System encountered an unexpected upstream error"
			} else if noiseType == 1 {
				// Overlapping code/message
				msg = fmt.Sprintf("Payment failed with notice: %s; verify account", prof.messages[r.Intn(len(prof.messages))])
			} else {
				code = "ACQUIRER_ERR_UNKNOWN"
			}
		}

		amount := float64((r.Intn(80) + 2) * 100) // ₹200 to ₹8,000
		txTime := now.Add(-time.Duration(*count-i) * (90 * time.Second))

		rawEvent := events.RawPaymentEvent{
			EventID:             evtID,
			TransactionID:       txID,
			MerchantID:          merch.id,
			EventType:           events.EventTypePaymentFailed,
			Amount:              amount,
			Currency:            "INR",
			PaymentMethod:       method,
			GatewayResponseCode: code,
			GatewayMessage:      msg,
			Timestamp:           txTime,
			AttemptNumber:       1,
			CustomerID:          fmt.Sprintf("cust_%04d", r.Intn(5000)+1000),
			Metadata: map[string]interface{}{
				"device":          "mobile_android",
				"acquirer":        "HDFC_DIRECT",
				"merchant_health": merch.health,
			},
		}

		gt := events.GroundTruth{
			TransactionID:        txID,
			TrueCause:            prof.trueCause,
			WasRecoverable:       prof.recoverable,
			TrueRecoveryAmount:   amount,
			BaseRecoverProb:      prof.baseProb,
			NoiseApplied:         isNoisy,
			GeneratedAt:          txTime.Format(time.RFC3339),
			MerchantHealthStatus: merch.health,
		}

		rawEvents = append(rawEvents, rawEvent)
		groundTruths = append(groundTruths, gt)
	}

	_ = os.MkdirAll(filepath.Dir(*outEvents), 0755)
	_ = os.MkdirAll(filepath.Dir(*outGroundTruth), 0755)

	writeJSONFile(*outEvents, rawEvents)
	writeJSONFile(*outGroundTruth, groundTruths)

	fmt.Printf("✓ Generated %d realistic noisy events -> %s\n", len(rawEvents), *outEvents)
	fmt.Printf("✓ Generated %d ground truth records -> %s\n", len(groundTruths), *outGroundTruth)
}

func writeJSONFile(path string, v interface{}) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file %s: %v\n", path, err)
		os.Exit(1)
	}
}
