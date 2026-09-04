package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"revenue-recovery-agent/internal/audit"
	"revenue-recovery-agent/internal/decide"
	"revenue-recovery-agent/internal/events"
	"revenue-recovery-agent/internal/state"
)

func main() {
	inputFile := flag.String("input", "data/synthetic/batch-01-classified.json", "Input classification records JSON file")
	eventsFile := flag.String("raw-events", "data/synthetic/batch-01.json", "Raw events JSON file for transaction attributes")
	outputFile := flag.String("output", "data/synthetic/batch-01-decisions.json", "Output decisions JSON file")
	auditFile := flag.String("audit-log", "data/audit.jsonl", "Path to audit log")
	stateFile := flag.String("state-file", "data/state-snapshot.json", "Path to state store snapshot")
	flag.Parse()

	data, err := os.ReadFile(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading classified file: %v\n", err)
		os.Exit(1)
	}

	var classifications []events.ClassificationRecord
	if err := json.Unmarshal(data, &classifications); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing classifications: %v\n", err)
		os.Exit(1)
	}

	// Load raw events map
	rawMap := make(map[string]events.RawPaymentEvent)
	if rawBytes, err := os.ReadFile(*eventsFile); err == nil {
		var rawList []events.RawPaymentEvent
		if err := json.Unmarshal(rawBytes, &rawList); err == nil {
			for _, r := range rawList {
				rawMap[r.TransactionID] = r
			}
		}
	}

	auditStore, err := audit.NewJSONLStore(*auditFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing audit store: %v\n", err)
		os.Exit(1)
	}
	defer auditStore.Close()

	stateStore, err := state.NewMemoryStateStore(*stateFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing state store: %v\n", err)
		os.Exit(1)
	}
	defer stateStore.Close()

	engine := decide.NewEngine()
	ctx := context.Background()

	var decisions []events.DecisionRecord
	var circuitBreakerCount int
	var complianceVetoCount int

	for _, cls := range classifications {
		raw := rawMap[cls.TransactionID]

		// Query real transaction state
		txHist, _ := stateStore.GetTransaction(ctx, cls.TransactionID)
		attemptCount := 0
		if txHist != nil {
			attemptCount = txHist.AttemptCount - 1 // zero-indexed previous attempts
		}

		// Query live merchant rolling failure rate over 15-minute window
		merchantFailRate, _, _ := stateStore.GetMerchantFailureRate(ctx, raw.MerchantID, 15*time.Minute)

		tCtx := decide.TransactionContext{
			AttemptCount:        attemptCount,
			MerchantID:          raw.MerchantID,
			MerchantFailureRate: merchantFailRate,
			PaymentMethod:       raw.PaymentMethod,
			Amount:              raw.Amount,
			Currency:            raw.Currency,
			FirstFailedAt:       raw.Timestamp,
		}

		decision, err := engine.Decide(ctx, cls, tCtx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Decision error for %s: %v\n", cls.TransactionID, err)
			continue
		}

		if decision.StoppingRuleTriggered != nil {
			if *decision.StoppingRuleTriggered == "merchant_circuit_breaker_tripped" {
				circuitBreakerCount++
			} else if *decision.StoppingRuleTriggered == "non_retryable_compliance_escalation" {
				complianceVetoCount++
			}
		}

		decisions = append(decisions, decision)

		payload, _ := json.Marshal(decision)
		_ = auditStore.Append(ctx, events.AuditLogEntry{
			AuditID:       fmt.Sprintf("aud_dec_%s", decision.TransactionID),
			CorrelationID: decision.TransactionID,
			TransactionID: decision.TransactionID,
			MerchantID:    raw.MerchantID,
			Stage:         events.StageDecide,
			Payload:       payload,
			Timestamp:     decision.DecidedAt,
		})
	}

	outBytes, _ := json.MarshalIndent(decisions, "", "  ")
	if err := os.WriteFile(*outputFile, outBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing decisions: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Generated %d Expected-Value decisions (CB Trips: %d, Compliance Vetoes: %d) -> %s\n",
		len(decisions), circuitBreakerCount, complianceVetoCount, *outputFile)
}
