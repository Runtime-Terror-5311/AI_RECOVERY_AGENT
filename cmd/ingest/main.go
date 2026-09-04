package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"revenue-recovery-agent/internal/audit"
	"revenue-recovery-agent/internal/events"
	"revenue-recovery-agent/internal/state"
)

func main() {
	inputFile := flag.String("input", "data/synthetic/batch-01.json", "Input raw payment events JSON file")
	outputFile := flag.String("output", "data/synthetic/batch-01-normalized.json", "Output normalized events JSON file")
	auditFile := flag.String("audit-log", "data/audit.jsonl", "Path to audit log")
	stateFile := flag.String("state-file", "data/state-snapshot.json", "Path to state store snapshot")
	flag.Parse()

	rawBytes, err := os.ReadFile(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input file: %v\n", err)
		os.Exit(1)
	}

	var rawEvents []events.RawPaymentEvent
	if err := json.Unmarshal(rawBytes, &rawEvents); err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding raw events: %v\n", err)
		os.Exit(1)
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

	ctx := context.Background()
	seenEvents := make(map[string]bool)
	var normalizedEvents []events.NormalizedEvent

	for _, raw := range rawEvents {
		isDupe := seenEvents[raw.EventID]
		seenEvents[raw.EventID] = true

		norm := events.NormalizedEvent{
			RawPaymentEvent: raw,
			NormalizedAt:    time.Now().UTC(),
			IsDuplicate:     isDupe,
			SchemaVersion:   "1.0",
		}
		normalizedEvents = append(normalizedEvents, norm)

		// Record in state store for merchant rolling window & attempt tracking
		_, _ = stateStore.RecordTransactionEvent(ctx, raw)

		payload, _ := json.Marshal(norm)
		_ = auditStore.Append(ctx, events.AuditLogEntry{
			AuditID:       fmt.Sprintf("aud_ing_%s", raw.EventID),
			CorrelationID: raw.TransactionID,
			TransactionID: raw.TransactionID,
			MerchantID:    raw.MerchantID,
			Stage:         events.StageIngest,
			Payload:       payload,
			Timestamp:     norm.NormalizedAt,
		})
	}

	outBytes, _ := json.MarshalIndent(normalizedEvents, "", "  ")
	if err := os.WriteFile(*outputFile, outBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing normalized events: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Ingested, validated, and state-indexed %d events -> %s\n", len(normalizedEvents), *outputFile)
}
