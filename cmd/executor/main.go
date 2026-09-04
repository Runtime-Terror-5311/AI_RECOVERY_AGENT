package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"revenue-recovery-agent/internal/audit"
	"revenue-recovery-agent/internal/events"
	"revenue-recovery-agent/internal/execute"
	"revenue-recovery-agent/internal/state"
)

func main() {
	inputFile := flag.String("input", "data/synthetic/batch-01-decisions.json", "Input decisions JSON file")
	eventsFile := flag.String("raw-events", "data/synthetic/batch-01.json", "Raw events JSON file for amount/method reference")
	outputFile := flag.String("output", "data/synthetic/batch-01-actions.json", "Output action results JSON file")
	auditFile := flag.String("audit-log", "data/audit.jsonl", "Path to audit log")
	stateFile := flag.String("state-file", "data/state-snapshot.json", "Path to state store snapshot")
	flag.Parse()

	decisionsData, err := os.ReadFile(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading decisions file: %v\n", err)
		os.Exit(1)
	}

	var decisions []events.DecisionRecord
	if err := json.Unmarshal(decisionsData, &decisions); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing decisions: %v\n", err)
		os.Exit(1)
	}

	// Map raw events to amounts and payment methods
	amounts := make(map[string]float64)
	methods := make(map[string]string)
	if rawData, err := os.ReadFile(*eventsFile); err == nil {
		var rawList []events.RawPaymentEvent
		if err := json.Unmarshal(rawData, &rawList); err == nil {
			for _, r := range rawList {
				amounts[r.TransactionID] = r.Amount
				methods[r.TransactionID] = r.PaymentMethod
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

	executor := execute.NewProbabilisticExecutor(200, 42) // 200 RPS, deterministic seed for reproducible batch
	ctx := context.Background()

	var results []events.ActionResult
	var totalRecovered float64
	var totalCost float64

	for _, dec := range decisions {
		amount := amounts[dec.TransactionID]
		method := methods[dec.TransactionID]

		res, err := executor.Execute(ctx, dec, amount, method)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Execution failed for txn %s: %v\n", dec.TransactionID, err)
			continue
		}
		results = append(results, res)
		totalRecovered += res.AmountRecovered
		totalCost += res.ActionCost

		// Update state store with execution outcome
		_ = stateStore.RecordActionResult(ctx, res)

		payload, _ := json.Marshal(res)
		_ = auditStore.Append(ctx, events.AuditLogEntry{
			AuditID:       fmt.Sprintf("aud_exe_%s", res.TransactionID),
			CorrelationID: res.TransactionID,
			TransactionID: res.TransactionID,
			MerchantID:    dec.MerchantID,
			Stage:         events.StageExecute,
			Payload:       payload,
			Timestamp:     res.ExecutedAt,
		})
	}

	outBytes, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(*outputFile, outBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing action results: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Executed %d actions (Recovered: ₹%.2f, Costs: ₹%.2f, Net: ₹%.2f) -> %s\n",
		len(results), totalRecovered, totalCost, totalRecovered-totalCost, *outputFile)
}
