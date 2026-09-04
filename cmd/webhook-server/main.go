package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"revenue-recovery-agent/internal/audit"
	"revenue-recovery-agent/internal/classify"
	"revenue-recovery-agent/internal/decide"
	"revenue-recovery-agent/internal/events"
	"revenue-recovery-agent/internal/execute"
	"revenue-recovery-agent/internal/razorpay"
)

func main() {
	port := flag.Int("port", 8080, "Port for Razorpay webhook HTTP listener")
	secret := flag.String("secret", os.Getenv("RAZORPAY_WEBHOOK_SECRET"), "Razorpay Webhook secret")
	auditFile := flag.String("audit-log", "data/audit.jsonl", "Path to audit log")
	flag.Parse()

	auditStore, err := audit.NewJSONLStore(*auditFile)
	if err != nil {
		log.Fatalf("Failed to initialize audit store: %v", err)
	}
	defer auditStore.Close()

	classifier := classify.NewRuleClassifier()
	engine := decide.NewEngine()
	executor := execute.NewProbabilisticExecutor(50, 0)

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Signature verification if secret is configured
		if *secret != "" {
			sig := r.Header.Get("X-Razorpay-Signature")
			if !razorpay.VerifyWebhookSignature(body, sig, *secret) {
				log.Printf("⚠️  Invalid webhook signature received!")
				http.Error(w, "Invalid signature", http.StatusUnauthorized)
				return
			}
		}

		wp, err := razorpay.ParseWebhook(body)
		if err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		log.Printf("📥 [Webhook Received] Event: %s | Account: %s", wp.Event, wp.AccountID)

		rawEvent, err := wp.ToRawPaymentEvent()
		if err != nil {
			log.Printf("⚠️  Ignored webhook without payment entity: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}

		ctx := context.Background()

		// 1. Audit Ingestion
		norm := events.NormalizedEvent{
			RawPaymentEvent: rawEvent,
			NormalizedAt:    rawEvent.Timestamp,
			IsDuplicate:     false,
			SchemaVersion:   "1.0",
		}
		payload, _ := json.Marshal(norm)
		_ = auditStore.Append(ctx, events.AuditLogEntry{
			AuditID:       fmt.Sprintf("aud_ing_%s", rawEvent.EventID),
			TransactionID: rawEvent.TransactionID,
			MerchantID:    rawEvent.MerchantID,
			Stage:         events.StageIngest,
			Payload:       payload,
			Timestamp:     norm.NormalizedAt,
		})

		// 2. Classify Root Cause
		cls, err := classifier.Classify(ctx, norm)
		if err != nil {
			log.Printf("Classifier error: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		log.Printf("🔍 [Classified] Txn: %s | Root Cause: %s (Confidence: %.2f)", cls.TransactionID, cls.RootCause, cls.Confidence)

		// 3. Make Bounded Recovery Decision
		tCtx := decide.TransactionContext{
			AttemptCount:        rawEvent.AttemptNumber - 1,
			MerchantID:          rawEvent.MerchantID,
			MerchantFailureRate: 0.05,
		}
		decision, err := engine.Decide(ctx, cls, tCtx)
		if err != nil {
			log.Printf("Decision error: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ruleMsg := "none"
		if decision.StoppingRuleTriggered != nil {
			ruleMsg = *decision.StoppingRuleTriggered
		}
		log.Printf("⚖️  [Decided] Action: %s | Stopping Rule: %s", decision.Action, ruleMsg)

		// 4. Execute Action
		actionRes, err := executor.Execute(ctx, decision, rawEvent.Amount, rawEvent.PaymentMethod)
		if err != nil {
			log.Printf("Executor error: %v", err)
		} else {
			log.Printf("⚡ [Executed] Result: %s | Recovered: INR %.2f", actionRes.ActionResult, actionRes.AmountRecovered)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":         "processed",
			"transaction_id": rawEvent.TransactionID,
			"root_cause":     cls.RootCause,
			"action":         decision.Action,
			"outcome":        actionRes.ActionResult,
		})
	})

	log.Printf("🚀 Razorpay Webhook Server listening on port :%d ...", *port)
	log.Printf("👉 Point Razorpay Dashboard webhook or ngrok to http://localhost:%d/webhook", *port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		log.Fatalf("Server stopped: %v", err)
	}
}
