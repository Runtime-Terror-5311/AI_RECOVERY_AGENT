package metrics

import (
	"fmt"
	"sort"

	"revenue-recovery-agent/internal/events"
)

// MetricsReport encapsulates the multi-dimensional honest performance report.
type MetricsReport struct {
	// Financial & Recovery
	TotalTransactions       int     `json:"total_transactions"`
	RecoverableTransactions int     `json:"recoverable_transactions"`
	TotalAmountAtRisk       float64 `json:"total_amount_at_risk"`
	GrossAmountRecovered    float64 `json:"gross_amount_recovered"`
	GrossRecoveryRate       float64 `json:"gross_recovery_rate"`
	TotalActionCosts        float64 `json:"total_action_costs"`
	FalseRetryCount         int     `json:"false_retry_count"`
	FalseRetryCost          float64 `json:"false_retry_cost"`
	NetRecoveredINR         float64 `json:"net_recovered_inr"`

	// 3-Policy Comparative Benchmarks
	PolicyBenchmarks []events.PolicyBenchmarkResult `json:"policy_benchmarks"`

	// Classification & ML
	MacroF1                float64                 `json:"macro_f1"`
	AbstentionRate         float64                 `json:"abstention_rate"`
	AbstainedCount         int                     `json:"abstained_count"`
	SelectiveAccuracy      float64                 `json:"selective_accuracy"`
	ClassificationByBucket map[string]BucketMetric `json:"classification_by_bucket"`
	ConfusionMatrix        map[string]map[string]int `json:"confusion_matrix"`

	// Decision & Safety
	ComplianceVetoCount     int     `json:"compliance_veto_count"`
	CircuitBreakerTrips     int     `json:"circuit_breaker_trips"`
	MaxRetriesBlocked       int     `json:"max_retries_blocked"`
	AverageExpectedNetValue float64 `json:"average_expected_net_value"`
	AverageRealizedNetValue float64 `json:"average_realized_net_value"`

	// Operational
	AverageOperationalLatencySec float64 `json:"average_operational_latency_sec"`
}

// BucketMetric measures precision, recall, and F1 for a single class.
type BucketMetric struct {
	TruePositives  int     `json:"true_positives"`
	FalsePositives int     `json:"false_positives"`
	FalseNegatives int     `json:"false_negatives"`
	Precision      float64 `json:"precision"`
	Recall         float64 `json:"recall"`
	F1             float64 `json:"f1"`
}

// Evaluator computes rigorous offline metrics and multi-policy benchmarks against ground truth.
type Evaluator struct {
	CostPerRetry float64
}

// NewEvaluator creates an evaluator instance.
func NewEvaluator(costPerRetry float64) *Evaluator {
	if costPerRetry <= 0 {
		costPerRetry = 2.50
	}
	return &Evaluator{CostPerRetry: costPerRetry}
}

// Compute processes the audit trail and evaluates model, policy, and financial quality against ground truth.
func (e *Evaluator) Compute(
	groundTruth map[string]events.GroundTruth,
	actions map[string]events.ActionResult,
	decisions map[string]events.DecisionRecord,
	classifications map[string]events.ClassificationRecord,
	rawEvents map[string]events.RawPaymentEvent,
) MetricsReport {
	report := MetricsReport{
		TotalTransactions:      len(groundTruth),
		ClassificationByBucket: make(map[string]BucketMetric),
		ConfusionMatrix:        make(map[string]map[string]int),
	}

	var totalRiskAmount float64
	var grossRecovered float64
	var totalActionCost float64
	var falseRetries int
	var totalExpectedNetValue float64
	var totalRealizedNetValue float64
	var totalOperationalLatency float64
	var successfulRecoveryCount int
	var abstainedCount int
	var correctNonAbstained int
	var totalNonAbstained int

	// Initialize Confusion Matrix
	for _, actual := range events.AllRootCauses {
		report.ConfusionMatrix[actual] = make(map[string]int)
		for _, pred := range events.AllRootCauses {
			report.ConfusionMatrix[actual][pred] = 0
		}
	}

	// 1. Process Financials, Decisions, and Model Quality
	for txID, gt := range groundTruth {
		if gt.WasRecoverable {
			report.RecoverableTransactions++
			totalRiskAmount += gt.TrueRecoveryAmount
		}

		// Classification stats
		if cls, ok := classifications[txID]; ok {
			if cls.Abstained {
				abstainedCount++
			} else {
				totalNonAbstained++
				if cls.RootCause == gt.TrueCause {
					correctNonAbstained++
				}
			}
			actual := gt.TrueCause
			pred := cls.RootCause
			report.ConfusionMatrix[actual][pred]++
		}

		// Decision stats
		if dec, ok := decisions[txID]; ok {
			totalExpectedNetValue += dec.ExpectedNetValue
			if dec.StoppingRuleTriggered != nil {
				rule := *dec.StoppingRuleTriggered
				if rule == "merchant_circuit_breaker_tripped" {
					report.CircuitBreakerTrips++
				} else if rule == "non_retryable_compliance_escalation" {
					report.ComplianceVetoCount++
				} else if rule == "max_retries_exceeded" {
					report.MaxRetriesBlocked++
				}
			}
		}

		// Action execution stats
		if act, ok := actions[txID]; ok {
			totalActionCost += act.ActionCost
			grossRecovered += act.AmountRecovered
			net := act.AmountRecovered - act.ActionCost
			totalRealizedNetValue += net

			if act.AmountRecovered > 0 {
				successfulRecoveryCount++
				totalOperationalLatency += act.OperationalRecoveryLatency
			}

			// False Retry Check
			if (act.Action == events.ActionRetryImmediate || act.Action == events.ActionRetryDelayed) && !gt.WasRecoverable {
				falseRetries++
			}
		}
	}

	report.TotalAmountAtRisk = totalRiskAmount
	report.GrossAmountRecovered = grossRecovered
	if totalRiskAmount > 0 {
		report.GrossRecoveryRate = (grossRecovered / totalRiskAmount) * 100
	}
	report.TotalActionCosts = totalActionCost
	report.FalseRetryCount = falseRetries
	report.FalseRetryCost = float64(falseRetries) * e.CostPerRetry
	report.NetRecoveredINR = grossRecovered - totalActionCost - report.FalseRetryCost
	report.AbstainedCount = abstainedCount
	if len(groundTruth) > 0 {
		report.AbstentionRate = (float64(abstainedCount) / float64(len(groundTruth))) * 100
		report.AverageExpectedNetValue = totalExpectedNetValue / float64(len(groundTruth))
		report.AverageRealizedNetValue = totalRealizedNetValue / float64(len(groundTruth))
	}
	if totalNonAbstained > 0 {
		report.SelectiveAccuracy = (float64(correctNonAbstained) / float64(totalNonAbstained)) * 100
	}
	if successfulRecoveryCount > 0 {
		report.AverageOperationalLatencySec = totalOperationalLatency / float64(successfulRecoveryCount)
	}

	// 2. Classification Metrics (Precision, Recall, F1, Macro-F1)
	var sumF1 float64
	var bucketCount int

	for _, cause := range events.AllRootCauses {
		tp := report.ConfusionMatrix[cause][cause]
		var fp int
		var fn int

		for _, otherCause := range events.AllRootCauses {
			if otherCause != cause {
				fp += report.ConfusionMatrix[otherCause][cause]
				fn += report.ConfusionMatrix[cause][otherCause]
			}
		}

		precision := 0.0
		if (tp + fp) > 0 {
			precision = float64(tp) / float64(tp+fp)
		}
		recall := 0.0
		if (tp + fn) > 0 {
			recall = float64(tp) / float64(tp+fn)
		}
		f1 := 0.0
		if (precision + recall) > 0 {
			f1 = 2 * (precision * recall) / (precision + recall)
		}

		if (tp + fn) > 0 {
			sumF1 += f1
			bucketCount++
		}

		report.ClassificationByBucket[cause] = BucketMetric{
			TruePositives:  tp,
			FalsePositives: fp,
			FalseNegatives: fn,
			Precision:      precision,
			Recall:         recall,
			F1:             f1,
		}
	}

	if bucketCount > 0 {
		report.MacroF1 = sumF1 / float64(bucketCount)
	}

	// 3. 3-Policy Comparative Benchmark Calculation
	// Policy 1: Control (No Action)
	controlBench := events.PolicyBenchmarkResult{
		PolicyName:         "Control (No Action)",
		PolicyDescription:  "Zero automated interventions (passive baseline)",
		GrossRecoveredINR:  0.0,
		GrossRecoveryRate:  0.0,
		TotalActionCostINR: 0.0,
		FalseRetriesCount:  0,
		FalseRetryCostINR:  0.0,
		NetRecoveredINR:    0.0,
		IncrementalLiftINR: 0.0,
		ComplianceVetoes:   0,
	}

	// Policy 2: Naive Blind Retry Policy (Retries everything 2 times blindly)
	var naiveGross float64
	var naiveCost float64
	var naiveFalseRetries int

	for _, gt := range groundTruth {
		// Naive retries 2 attempts (2 * 2.50 = 5.00 cost per tx)
		naiveCost += 5.00
		if gt.WasRecoverable {
			// Recovers with standard probability (~65%)
			naiveGross += gt.TrueRecoveryAmount * 0.65
		} else {
			// Unrecoverable transactions still get retried -> 2 false retries per tx!
			naiveFalseRetries += 2
		}
	}

	naiveFalseRetryCost := float64(naiveFalseRetries) * e.CostPerRetry
	naiveNet := naiveGross - naiveCost - naiveFalseRetryCost
	naiveRate := 0.0
	if totalRiskAmount > 0 {
		naiveRate = (naiveGross / totalRiskAmount) * 100
	}

	naiveBench := events.PolicyBenchmarkResult{
		PolicyName:         "Naive Blind Retries",
		PolicyDescription:  "Blindly retries all errors twice without ML diagnosis or safety veto",
		GrossRecoveredINR:  naiveGross,
		GrossRecoveryRate:  naiveRate,
		TotalActionCostINR: naiveCost,
		FalseRetriesCount:  naiveFalseRetries,
		FalseRetryCostINR:  naiveFalseRetryCost,
		NetRecoveredINR:    naiveNet,
		IncrementalLiftINR: naiveNet,
		ComplianceVetoes:   0,
	}

	// Policy 3: AI Constrained ENV Agent
	aiBench := events.PolicyBenchmarkResult{
		PolicyName:         "AI Constrained ENV Agent",
		PolicyDescription:  "ML diagnosis + Expected Net Value optimization + Deterministic safety policy",
		GrossRecoveredINR:  report.GrossAmountRecovered,
		GrossRecoveryRate:  report.GrossRecoveryRate,
		TotalActionCostINR: report.TotalActionCosts,
		FalseRetriesCount:  report.FalseRetryCount,
		FalseRetryCostINR:  report.FalseRetryCost,
		NetRecoveredINR:    report.NetRecoveredINR,
		IncrementalLiftINR: report.NetRecoveredINR - naiveNet,
		ComplianceVetoes:   report.ComplianceVetoCount,
	}

	report.PolicyBenchmarks = []events.PolicyBenchmarkResult{controlBench, naiveBench, aiBench}

	return report
}

// PrintSummary outputs a structured, human-readable terminal report.
func (r *MetricsReport) PrintSummary() {
	fmt.Println("==========================================================================")
	fmt.Println("             AI REVENUE RECOVERY AGENT — EVALUATION REPORT                ")
	fmt.Println("==========================================================================")

	fmt.Println("\n📊 1. 3-POLICY COMPARATIVE BENCHMARK (Economic Lift Proof)")
	fmt.Println(" -----------------------------------------------------------------------------------------")
	fmt.Printf(" %-26s %-14s %-12s %-14s %-14s\n", "Policy", "Gross Recov", "Action Cost", "False Retries", "Net Revenue")
	fmt.Println(" -----------------------------------------------------------------------------------------")
	for _, b := range r.PolicyBenchmarks {
		fmt.Printf(" • %-24s ₹%-13.2f -₹%-10.2f %-14d ₹%-13.2f\n",
			b.PolicyName, b.GrossRecoveredINR, b.TotalActionCostINR, b.FalseRetriesCount, b.NetRecoveredINR)
	}
	fmt.Println(" -----------------------------------------------------------------------------------------")
	if len(r.PolicyBenchmarks) >= 3 {
		fmt.Printf(" 🏆 NET FINANCIAL LIFT OF AI AGENT: +₹%.2f vs Naive Policy\n", r.PolicyBenchmarks[2].IncrementalLiftINR)
	}

	fmt.Println("\n🧠 2. ROOT CAUSE CLASSIFICATION & ML METRICS (Model A)")
	fmt.Printf(" • Macro-F1 Score:               %.3f\n", r.MacroF1)
	fmt.Printf(" • Selective Accuracy (No Abst): %.1f%%\n", r.SelectiveAccuracy)
	fmt.Printf(" • Abstention / Review Rate:     %.1f%% (%d cases routed to review)\n", r.AbstentionRate, r.AbstainedCount)
	fmt.Println(" ------------------------------------------------------------------------")
	fmt.Printf(" %-22s %-10s %-10s %-8s (TP / FP / FN)\n", "Root Cause", "Precision", "Recall", "F1")
	fmt.Println(" ------------------------------------------------------------------------")

	var sortedCauses []string
	for c := range r.ClassificationByBucket {
		sortedCauses = append(sortedCauses, c)
	}
	sort.Strings(sortedCauses)

	for _, cause := range sortedCauses {
		b := r.ClassificationByBucket[cause]
		if b.TruePositives+b.FalseNegatives > 0 || b.FalsePositives > 0 {
			fmt.Printf(" • %-20s %-10.2f %-10.2f %-8.2f (TP:%d, FP:%d, FN:%d)\n",
				cause, b.Precision, b.Recall, b.F1, b.TruePositives, b.FalsePositives, b.FalseNegatives)
		}
	}

	fmt.Println("\n🛡️ 3. SAFETY POLICY & COMPLIANCE BOUNDS")
	fmt.Printf(" • Compliance Vetoes (Risk/Auth): %d (Zero retries enforced)\n", r.ComplianceVetoCount)
	fmt.Printf(" • Circuit Breaker Activations:   %d (Merchant failure > 40%% halted)\n", r.CircuitBreakerTrips)
	fmt.Printf(" • Max Retries Blocked:           %d\n", r.MaxRetriesBlocked)

	fmt.Println("\n⏱️ 4. OPERATIONAL LATENCY")
	fmt.Printf(" • Avg Operational Cooldown:     %.1f seconds (scheduled batch timeline)\n", r.AverageOperationalLatencySec)
	fmt.Println("==========================================================================")
}
