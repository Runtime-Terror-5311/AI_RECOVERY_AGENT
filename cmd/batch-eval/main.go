package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"revenue-recovery-agent/internal/events"
	"revenue-recovery-agent/internal/metrics"
)

func main() {
	groundTruthFile := flag.String("ground-truth", "data/ground-truth/batch-01.json", "Ground truth JSON file")
	actionsFile := flag.String("actions", "data/synthetic/batch-01-actions.json", "Action results JSON file")
	decisionsFile := flag.String("decisions", "data/synthetic/batch-01-decisions.json", "Decisions JSON file")
	classificationsFile := flag.String("classifications", "data/synthetic/batch-01-classified.json", "Classifications JSON file")
	rawEventsFile := flag.String("raw-events", "data/synthetic/batch-01.json", "Raw events JSON file")
	costPerRetry := flag.Float64("cost-per-retry", 2.50, "Cost per retry attempt in INR")
	outReport := flag.String("out-report", "data/evaluation-report.json", "Output JSON report path")
	flag.Parse()

	// Load ground truth
	gtData, err := os.ReadFile(*groundTruthFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read ground truth: %v\n", err)
		os.Exit(1)
	}
	var gtList []events.GroundTruth
	if err := json.Unmarshal(gtData, &gtList); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse ground truth: %v\n", err)
		os.Exit(1)
	}
	gtMap := make(map[string]events.GroundTruth)
	for _, gt := range gtList {
		gtMap[gt.TransactionID] = gt
	}

	// Load actions
	actionsMap := make(map[string]events.ActionResult)
	if actData, err := os.ReadFile(*actionsFile); err == nil {
		var actList []events.ActionResult
		if err := json.Unmarshal(actData, &actList); err == nil {
			for _, a := range actList {
				actionsMap[a.TransactionID] = a
			}
		}
	}

	// Load decisions
	decisionsMap := make(map[string]events.DecisionRecord)
	if decData, err := os.ReadFile(*decisionsFile); err == nil {
		var decList []events.DecisionRecord
		if err := json.Unmarshal(decData, &decList); err == nil {
			for _, d := range decList {
				decisionsMap[d.TransactionID] = d
			}
		}
	}

	// Load classifications
	clsMap := make(map[string]events.ClassificationRecord)
	if clsData, err := os.ReadFile(*classificationsFile); err == nil {
		var clsList []events.ClassificationRecord
		if err := json.Unmarshal(clsData, &clsList); err == nil {
			for _, c := range clsList {
				clsMap[c.TransactionID] = c
			}
		}
	}

	// Load raw events
	rawMap := make(map[string]events.RawPaymentEvent)
	if rawData, err := os.ReadFile(*rawEventsFile); err == nil {
		var rawList []events.RawPaymentEvent
		if err := json.Unmarshal(rawData, &rawList); err == nil {
			for _, r := range rawList {
				rawMap[r.TransactionID] = r
			}
		}
	}

	evaluator := metrics.NewEvaluator(*costPerRetry)
	report := evaluator.Compute(gtMap, actionsMap, decisionsMap, clsMap, rawMap)

	// Print formatted summary
	report.PrintSummary()

	// Save JSON report
	if reportBytes, err := json.MarshalIndent(report, "", "  "); err == nil {
		_ = os.WriteFile(*outReport, reportBytes, 0644)
		fmt.Printf("\n✓ Full evaluation report JSON saved -> %s\n", *outReport)
	}
}
