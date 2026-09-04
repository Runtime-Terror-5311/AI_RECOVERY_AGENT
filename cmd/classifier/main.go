package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"revenue-recovery-agent/internal/audit"
	"revenue-recovery-agent/internal/classify"
	"revenue-recovery-agent/internal/events"
)

func main() {
	inputFile := flag.String("input", "data/synthetic/batch-01-normalized.json", "Input normalized events JSON file")
	outputFile := flag.String("output", "data/synthetic/batch-01-classified.json", "Output classifications JSON file")
	auditFile := flag.String("audit-log", "data/audit.jsonl", "Path to audit log")
	modelType := flag.String("model", "ml", "Model to use: 'ml' (Probabilistic Feature Classifier) or 'rule' (Baseline)")
	threshold := flag.Float64("abstain-threshold", 0.45, "Confidence threshold below which model abstains to 'unknown'")
	flag.Parse()

	data, err := os.ReadFile(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading normalized file: %v\n", err)
		os.Exit(1)
	}

	var normEvents []events.NormalizedEvent
	if err := json.Unmarshal(data, &normEvents); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing normalized events: %v\n", err)
		os.Exit(1)
	}

	auditStore, err := audit.NewJSONLStore(*auditFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing audit store: %v\n", err)
		os.Exit(1)
	}
	defer auditStore.Close()

	var classifier classify.Classifier
	if *modelType == "rule" {
		classifier = classify.NewRuleClassifier()
	} else {
		classifier = classify.NewMLClassifier(*threshold)
	}

	ctx := context.Background()
	var classifications []events.ClassificationRecord
	var abstainedCount int

	for _, ne := range normEvents {
		rec, err := classifier.Classify(ctx, ne)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Classification failed for txn %s: %v\n", ne.TransactionID, err)
			continue
		}
		if rec.Abstained {
			abstainedCount++
		}
		classifications = append(classifications, rec)

		payload, _ := json.Marshal(rec)
		_ = auditStore.Append(ctx, events.AuditLogEntry{
			AuditID:       fmt.Sprintf("aud_cls_%s", rec.EventID),
			CorrelationID: rec.TransactionID,
			TransactionID: rec.TransactionID,
			MerchantID:    ne.MerchantID,
			Stage:         events.StageClassify,
			Payload:       payload,
			Timestamp:     rec.ClassifiedAt,
		})
	}

	outBytes, _ := json.MarshalIndent(classifications, "", "  ")
	if err := os.WriteFile(*outputFile, outBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing classifications: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Classified %d events using [%s] (Abstained: %d) -> %s\n", len(classifications), classifier.ModelID(), abstainedCount, *outputFile)
}
