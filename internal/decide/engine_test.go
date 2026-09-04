package decide

import (
	"context"
	"testing"
	"time"

	"revenue-recovery-agent/internal/events"
)

func TestDecisionEngine(t *testing.T) {
	engine := NewEngine()
	ctx := context.Background()

	t.Run("Merchant circuit breaker trips when rolling failure rate > 40%", func(t *testing.T) {
		record := events.ClassificationRecord{
			TransactionID: "txn_cb_01",
			RootCause:     events.RootCauseGatewayTimeout,
		}
		tCtx := TransactionContext{
			AttemptCount:        0,
			MerchantID:          "merch_failing_store",
			MerchantFailureRate: 0.48, // 48% failure rate
			PaymentMethod:       events.PaymentMethodUPI,
			Amount:              3500.00,
			FirstFailedAt:       time.Now(),
		}

		decision, err := engine.Decide(ctx, record, tCtx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if decision.Action != events.ActionHaltMerchant {
			t.Errorf("Expected action %s, got %s", events.ActionHaltMerchant, decision.Action)
		}
		if decision.StoppingRuleTriggered == nil || *decision.StoppingRuleTriggered != "merchant_circuit_breaker_tripped" {
			t.Errorf("Expected merchant_circuit_breaker_tripped rule trigger")
		}
	})

	t.Run("Compliance Veto for risk_block routes strictly to escalate_human", func(t *testing.T) {
		record := events.ClassificationRecord{
			TransactionID: "txn_risk_01",
			RootCause:     events.RootCauseRiskBlock,
		}
		tCtx := TransactionContext{
			AttemptCount:        0,
			MerchantID:          "merch_healthy",
			MerchantFailureRate: 0.02,
			PaymentMethod:       events.PaymentMethodCard,
			Amount:              8000.00,
			FirstFailedAt:       time.Now(),
		}

		decision, err := engine.Decide(ctx, record, tCtx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if decision.Action != events.ActionEscalateHuman {
			t.Errorf("Expected action %s, got %s", events.ActionEscalateHuman, decision.Action)
		}
		if decision.StoppingRuleTriggered == nil || *decision.StoppingRuleTriggered != "non_retryable_compliance_escalation" {
			t.Errorf("Expected non_retryable_compliance_escalation")
		}
	})

	t.Run("Max retries cap enforces escalate_human after attempt 3", func(t *testing.T) {
		record := events.ClassificationRecord{
			TransactionID: "txn_retry_cap_01",
			RootCause:     events.RootCauseGatewayTimeout,
		}
		tCtx := TransactionContext{
			AttemptCount:        3, // reached max cap
			MerchantID:          "merch_healthy",
			MerchantFailureRate: 0.03,
			PaymentMethod:       events.PaymentMethodUPI,
			Amount:              1200.00,
			FirstFailedAt:       time.Now().Add(-10 * time.Minute),
		}

		decision, err := engine.Decide(ctx, record, tCtx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if decision.Action != events.ActionEscalateHuman {
			t.Errorf("Expected action %s, got %s", events.ActionEscalateHuman, decision.Action)
		}
		if decision.StoppingRuleTriggered == nil || *decision.StoppingRuleTriggered != "max_retries_exceeded" {
			t.Errorf("Expected max_retries_exceeded rule")
		}
	})

	t.Run("Expected Net Value selects retry_delayed with cooldown for issuer_decline", func(t *testing.T) {
		record := events.ClassificationRecord{
			TransactionID: "txn_issuer_01",
			RootCause:     events.RootCauseIssuerDecline,
		}
		tCtx := TransactionContext{
			AttemptCount:        0,
			MerchantID:          "merch_healthy",
			MerchantFailureRate: 0.05,
			PaymentMethod:       events.PaymentMethodCard,
			Amount:              4500.00,
			FirstFailedAt:       time.Now(),
		}

		decision, err := engine.Decide(ctx, record, tCtx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if decision.Action != events.ActionRetryDelayed {
			t.Errorf("Expected action %s, got %s", events.ActionRetryDelayed, decision.Action)
		}
		if decision.CooldownSeconds != 900 {
			t.Errorf("Expected 900s cooldown for issuer_decline, got %d", decision.CooldownSeconds)
		}
		if decision.ExpectedNetValue <= 0 {
			t.Errorf("Expected positive expected net value, got %.2f", decision.ExpectedNetValue)
		}
	})

	t.Run("Expired card triggers send_update_card_link for card payments", func(t *testing.T) {
		record := events.ClassificationRecord{
			TransactionID: "txn_expired_01",
			RootCause:     events.RootCauseExpiredCard,
		}
		tCtx := TransactionContext{
			AttemptCount:        0,
			MerchantID:          "merch_healthy",
			MerchantFailureRate: 0.05,
			PaymentMethod:       events.PaymentMethodCard,
			Amount:              2999.00,
			FirstFailedAt:       time.Now(),
		}

		decision, err := engine.Decide(ctx, record, tCtx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if decision.Action != events.ActionSendUpdateCardLink {
			t.Errorf("Expected action %s, got %s", events.ActionSendUpdateCardLink, decision.Action)
		}
	})
}
