package decide

import (
	"context"
	"fmt"
	"time"

	"revenue-recovery-agent/internal/events"
)

// TransactionContext captures dynamic operational and merchant state.
type TransactionContext struct {
	AttemptCount        int
	MerchantID          string
	MerchantFailureRate float64
	PaymentMethod       string
	Amount              float64
	Currency            string
	FirstFailedAt       time.Time
	PriorActions        []string
}

// ActionCostProfile defines financial costs, customer friction, and risk penalties for an action.
type ActionCostProfile struct {
	ActionCost           float64
	CustomerFrictionCost float64
	RiskPenalty          float64
}

// Engine implements Expected Net Value optimization under deterministic safety policy constraints.
type Engine struct {
	MaxRetries               int
	MerchantBreakerThreshold float64
	PolicyVersion            string
	ModelID                  string
	CostProfiles             map[string]ActionCostProfile
}

// NewEngine initializes the Decision Engine.
func NewEngine() *Engine {
	return &Engine{
		MaxRetries:               3,
		MerchantBreakerThreshold: 0.40,
		PolicyVersion:            "constrained-policy-v2.5-buildathon",
		ModelID:                  "model_b_recovery_prob_v2.0",
		CostProfiles: map[string]ActionCostProfile{
			events.ActionRetryImmediate:     {ActionCost: 2.50, CustomerFrictionCost: 0.00, RiskPenalty: 0.00},
			events.ActionRetryDelayed:       {ActionCost: 2.50, CustomerFrictionCost: 0.00, RiskPenalty: 0.00},
			events.ActionSendUpdateCardLink: {ActionCost: 1.00, CustomerFrictionCost: 5.00, RiskPenalty: 0.00},
			events.ActionEscalateHuman:      {ActionCost: 25.00, CustomerFrictionCost: 0.00, RiskPenalty: 0.00},
			events.ActionNoAction:           {ActionCost: 0.00, CustomerFrictionCost: 0.00, RiskPenalty: 0.00},
			events.ActionHaltMerchant:       {ActionCost: 0.00, CustomerFrictionCost: 0.00, RiskPenalty: 0.00},
		},
	}
}

// PredictRecoveryProbability calculates Model B probability estimate P(success | cause, action, attempt, method).
func (e *Engine) PredictRecoveryProbability(cause string, action string, attempt int, method string) float64 {
	if cause == events.RootCauseRiskBlock || cause == events.RootCauseInvalidCredentials {
		if action == events.ActionEscalateHuman {
			return 0.15
		}
		return 0.00
	}

	if cause == events.RootCauseExpiredCard {
		if action == events.ActionSendUpdateCardLink && (method == events.PaymentMethodCard || method == events.PaymentMethodEmandate) {
			return 0.45
		}
		return 0.00
	}

	if cause == events.RootCauseGatewayTimeout || cause == events.RootCauseNetworkError {
		switch action {
		case events.ActionRetryImmediate:
			if attempt == 0 {
				return 0.78
			}
			return 0.25
		case events.ActionRetryDelayed:
			if attempt == 0 {
				return 0.74
			} else if attempt == 1 {
				return 0.45
			} else {
				return 0.18
			}
		case events.ActionEscalateHuman:
			return 0.10
		default:
			return 0.00
		}
	}

	if cause == events.RootCauseIssuerDecline {
		switch action {
		case events.ActionRetryDelayed:
			if attempt == 0 {
				return 0.68
			} else if attempt == 1 {
				return 0.35
			} else {
				return 0.12
			}
		case events.ActionSendUpdateCardLink:
			return 0.25
		case events.ActionEscalateHuman:
			return 0.10
		default:
			return 0.00
		}
	}

	if cause == events.RootCauseInsufficientFunds {
		switch action {
		case events.ActionRetryDelayed:
			if attempt == 0 {
				return 0.62
			} else if attempt == 1 {
				return 0.30
			} else {
				return 0.10
			}
		case events.ActionSendUpdateCardLink:
			return 0.20
		default:
			return 0.00
		}
	}

	if cause == events.RootCauseUnknown {
		if action == events.ActionEscalateHuman {
			return 0.25
		}
		return 0.05
	}

	return 0.00
}

// Decide processes a classification record, checks safety constraints, evaluates Expected Net Value, and selects the optimal action.
func (e *Engine) Decide(ctx context.Context, record events.ClassificationRecord, tCtx TransactionContext) (events.DecisionRecord, error) {
	now := time.Now().UTC()

	// -------------------------------------------------------------
	// 1. HARD DETERMINISTIC SAFETY CONSTRAINTS
	// -------------------------------------------------------------

	// Constraint 1: Merchant Circuit Breaker (Merchant-wide degradation)
	if tCtx.MerchantFailureRate > e.MerchantBreakerThreshold {
		rule := "merchant_circuit_breaker_tripped"
		reason := fmt.Sprintf("Merchant rolling failure rate (%.1f%%) exceeds 40%% threshold; automated retries halted", tCtx.MerchantFailureRate*100)
		return events.DecisionRecord{
			TransactionID:         record.TransactionID,
			MerchantID:            tCtx.MerchantID,
			RootCause:             record.RootCause,
			Action:                events.ActionHaltMerchant,
			DecisionType:          events.DecisionTypeCircuitHalted,
			ExpectedNetValue:      0.0,
			DecidedAt:             now,
			PlannedExecutionTime:  now,
			CooldownSeconds:       0,
			StoppingRuleTriggered: &rule,
			PolicyVersion:         e.PolicyVersion,
			ModelID:               e.ModelID,
			DecisionReason:        reason,
		}, nil
	}

	// Constraint 2: Compliance Veto for Security / Fraud / Auth Failures
	if record.RootCause == events.RootCauseRiskBlock || record.RootCause == events.RootCauseInvalidCredentials {
		rule := "non_retryable_compliance_escalation"
		reason := fmt.Sprintf("Compliance Policy Veto: cause '%s' is strictly non-retryable; routed to human review", record.RootCause)
		return events.DecisionRecord{
			TransactionID:         record.TransactionID,
			MerchantID:            tCtx.MerchantID,
			RootCause:             record.RootCause,
			Action:                events.ActionEscalateHuman,
			DecisionType:          events.DecisionTypeVetoed,
			ExpectedNetValue:      -e.CostProfiles[events.ActionEscalateHuman].ActionCost,
			DecidedAt:             now,
			PlannedExecutionTime:  now,
			CooldownSeconds:       0,
			StoppingRuleTriggered: &rule,
			PolicyVersion:         e.PolicyVersion,
			ModelID:               e.ModelID,
			DecisionReason:        reason,
		}, nil
	}

	// Constraint 3: Max Retries Cap
	if tCtx.AttemptCount >= e.MaxRetries {
		rule := "max_retries_exceeded"
		reason := fmt.Sprintf("Safety Bound: Max retry cap (%d/%d) exhausted; escalated to human review", tCtx.AttemptCount, e.MaxRetries)
		return events.DecisionRecord{
			TransactionID:         record.TransactionID,
			MerchantID:            tCtx.MerchantID,
			RootCause:             record.RootCause,
			Action:                events.ActionEscalateHuman,
			DecisionType:          events.DecisionTypeVetoed,
			ExpectedNetValue:      -e.CostProfiles[events.ActionEscalateHuman].ActionCost,
			DecidedAt:             now,
			PlannedExecutionTime:  now,
			CooldownSeconds:       0,
			StoppingRuleTriggered: &rule,
			PolicyVersion:         e.PolicyVersion,
			ModelID:               e.ModelID,
			DecisionReason:        reason,
		}, nil
	}

	// Constraint 4: Selective Abstention on Low-Confidence / Uncertain Diagnosis
	if record.Abstained {
		rule := "uncertainty_abstention_for_review"
		reason := fmt.Sprintf("Selective Abstention: Model confidence (%.2f) below safety threshold; routed to human review queue", record.Confidence)
		return events.DecisionRecord{
			TransactionID:         record.TransactionID,
			MerchantID:            tCtx.MerchantID,
			RootCause:             record.RootCause,
			Action:                events.ActionEscalateHuman,
			DecisionType:          events.DecisionTypeAbstained,
			ExpectedNetValue:      -e.CostProfiles[events.ActionEscalateHuman].ActionCost,
			DecidedAt:             now,
			PlannedExecutionTime:  now,
			CooldownSeconds:       0,
			StoppingRuleTriggered: &rule,
			PolicyVersion:         e.PolicyVersion,
			ModelID:               e.ModelID,
			DecisionReason:        reason,
		}, nil
	}

	// -------------------------------------------------------------
	// 2. EXPECTED NET VALUE (ENV) OPTIMIZATION
	// -------------------------------------------------------------
	var candidateScores []events.CandidateActionScore
	var bestScore float64 = -1e9
	var bestAction string = events.ActionNoAction
	var bestCooldown int = 0
	var chosenReason string

	for _, act := range events.AllActions {
		if act == events.ActionHaltMerchant {
			continue
		}

		profile := e.CostProfiles[act]
		prob := e.PredictRecoveryProbability(record.RootCause, act, tCtx.AttemptCount, tCtx.PaymentMethod)
		grossExpected := prob * tCtx.Amount
		env := grossExpected - profile.ActionCost - profile.CustomerFrictionCost - profile.RiskPenalty

		policyAllowed := true
		var vetoReason string

		if act == events.ActionSendUpdateCardLink && tCtx.PaymentMethod != events.PaymentMethodCard && tCtx.PaymentMethod != events.PaymentMethodEmandate {
			policyAllowed = false
			vetoReason = fmt.Sprintf("Action not eligible for payment method '%s'", tCtx.PaymentMethod)
		}

		score := events.CandidateActionScore{
			Action:               act,
			PredictedSuccessProb: prob,
			ExpectedGrossRecover: grossExpected,
			ActionCost:           profile.ActionCost,
			CustomerFrictionCost: profile.CustomerFrictionCost,
			RiskPenalty:          profile.RiskPenalty,
			ExpectedNetValue:     env,
			PolicyAllowed:        policyAllowed,
			VetoReason:           vetoReason,
		}
		candidateScores = append(candidateScores, score)

		if policyAllowed && env > bestScore {
			bestScore = env
			bestAction = act
		}
	}

	var actionParams *events.ActionParams
	var stoppingRule *string

	switch bestAction {
	case events.ActionRetryImmediate:
		bestCooldown = 0
		actionParams = &events.ActionParams{DelaySeconds: 0, RetryCount: tCtx.AttemptCount + 1}
		chosenReason = fmt.Sprintf("Immediate retry optimal: ENV=₹%.2f (P(success)=%.2f)", bestScore, e.PredictRecoveryProbability(record.RootCause, bestAction, tCtx.AttemptCount, tCtx.PaymentMethod))

	case events.ActionRetryDelayed:
		if record.RootCause == events.RootCauseInsufficientFunds {
			bestCooldown = 3600
		} else if record.RootCause == events.RootCauseIssuerDecline {
			bestCooldown = 900
		} else {
			bestCooldown = 300
		}
		actionParams = &events.ActionParams{DelaySeconds: bestCooldown, RetryCount: tCtx.AttemptCount + 1}
		chosenReason = fmt.Sprintf("Delayed retry (%ds cooldown) optimal: ENV=₹%.2f", bestCooldown, bestScore)

	case events.ActionSendUpdateCardLink:
		bestCooldown = 0
		chosenReason = fmt.Sprintf("Update card link dispatched: customer card expired/invalid (ENV=₹%.2f)", bestScore)

	case events.ActionEscalateHuman:
		bestCooldown = 0
		rule := "low_expected_value_escalation"
		stoppingRule = &rule
		chosenReason = "Escalated to human review: automated action has negative or negligible expected value"

	default:
		bestCooldown = 0
		chosenReason = "No automated recovery action justified by economics"
	}

	plannedExecution := now.Add(time.Duration(bestCooldown) * time.Second)

	return events.DecisionRecord{
		TransactionID:         record.TransactionID,
		MerchantID:            tCtx.MerchantID,
		RootCause:             record.RootCause,
		Action:                bestAction,
		DecisionType:          events.DecisionTypeAutomated,
		ActionParams:          actionParams,
		CandidateScores:       candidateScores,
		ExpectedNetValue:      bestScore,
		DecidedAt:             now,
		PlannedExecutionTime:  plannedExecution,
		CooldownSeconds:       bestCooldown,
		StoppingRuleTriggered: stoppingRule,
		PolicyVersion:         e.PolicyVersion,
		ModelID:               e.ModelID,
		DecisionReason:        chosenReason,
	}, nil
}
