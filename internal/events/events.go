package events

import (
	"encoding/json"
	"time"
)

// Event Types
const (
	EventTypePaymentInitiated = "payment_initiated"
	EventTypePaymentFailed    = "payment_failed"
	EventTypePaymentSuccess   = "payment_success"
	EventTypePaymentTimeout   = "payment_timeout"
	EventTypeRetryAttempted   = "retry_attempted"
)

// Payment Methods
const (
	PaymentMethodCard       = "card"
	PaymentMethodUPI        = "upi"
	PaymentMethodNetbanking = "netbanking"
	PaymentMethodWallet     = "wallet"
	PaymentMethodEmandate   = "emandate"
)

// Root Cause Taxonomy
const (
	RootCauseIssuerDecline      = "issuer_decline"
	RootCauseInsufficientFunds  = "insufficient_funds"
	RootCauseExpiredCard        = "expired_card"
	RootCauseGatewayTimeout     = "gateway_timeout"
	RootCauseNetworkError       = "network_error"
	RootCauseRiskBlock          = "risk_block"
	RootCauseInvalidCredentials = "invalid_credentials"
	RootCauseUnknown            = "unknown"
)

// Special Class Tag for Uncertainty Refusal
const (
	StatusAbstainForReview = "ABSTAIN_FOR_REVIEW"
)

// Canonical list of root causes
var AllRootCauses = []string{
	RootCauseIssuerDecline,
	RootCauseInsufficientFunds,
	RootCauseExpiredCard,
	RootCauseGatewayTimeout,
	RootCauseNetworkError,
	RootCauseRiskBlock,
	RootCauseInvalidCredentials,
	RootCauseUnknown,
}

// Action Taxonomy
const (
	ActionRetryImmediate     = "retry_immediate"
	ActionRetryDelayed       = "retry_delayed"
	ActionSendUpdateCardLink = "send_update_card_link"
	ActionEscalateHuman      = "escalate_human"
	ActionNoAction           = "no_action"
	ActionHaltMerchant       = "halt_merchant"
)

var AllActions = []string{
	ActionRetryImmediate,
	ActionRetryDelayed,
	ActionSendUpdateCardLink,
	ActionEscalateHuman,
	ActionNoAction,
	ActionHaltMerchant,
}

// Action Execution Results
const (
	ActionResultSuccess = "success"
	ActionResultFailed  = "failed"
	ActionResultSkipped = "skipped"
)

// Decision Types
const (
	DecisionTypeAutomated     = "AUTOMATED"
	DecisionTypeAbstained     = "ABSTAINED"
	DecisionTypeVetoed        = "VETOED"
	DecisionTypeCircuitHalted = "CIRCUIT_HALTED"
)

// Audit Stages
const (
	StageIngest   = "ingest"
	StageClassify = "classify"
	StageDecide   = "decide"
	StageExecute  = "execute"
)

// RawPaymentEvent represents the contract emitted by Data Generator or Webhook Ingestion.
type RawPaymentEvent struct {
	EventID             string                 `json:"event_id"`
	TransactionID       string                 `json:"transaction_id"`
	MerchantID          string                 `json:"merchant_id"`
	EventType           string                 `json:"event_type"`
	Amount              float64                `json:"amount"`
	Currency            string                 `json:"currency"`
	PaymentMethod       string                 `json:"payment_method"`
	GatewayResponseCode string                 `json:"gateway_response_code"`
	GatewayMessage      string                 `json:"gateway_message"`
	Timestamp           time.Time              `json:"timestamp"`
	AttemptNumber       int                    `json:"attempt_number"`
	CustomerID          string                 `json:"customer_id"`
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
}

// NormalizedEvent is produced by Ingestion and consumed by Classifier.
type NormalizedEvent struct {
	RawPaymentEvent
	NormalizedAt  time.Time `json:"normalized_at"`
	IsDuplicate   bool      `json:"is_duplicate"`
	SchemaVersion string    `json:"schema_version"`
}

// ClassificationRecord is produced by Classifier and consumed by Decision Engine.
type ClassificationRecord struct {
	EventID           string             `json:"event_id"`
	TransactionID     string             `json:"transaction_id"`
	RootCause         string             `json:"root_cause"`
	Confidence        float64            `json:"confidence"`
	Probabilities     map[string]float64 `json:"probabilities,omitempty"`
	ClassifiedAt      time.Time          `json:"classified_at"`
	ClassifierVersion string             `json:"classifier_version"`
	ModelID           string             `json:"model_id,omitempty"`
	Abstained         bool               `json:"abstained"`
	AbstentionReason  string             `json:"abstention_reason,omitempty"`
	FeaturesExtracted map[string]float64 `json:"features_extracted,omitempty"`
}

// ActionParams captures parameters for executing a decided action.
type ActionParams struct {
	DelaySeconds int `json:"delay_seconds,omitempty"`
	RetryCount   int `json:"retry_count,omitempty"`
}

// CandidateActionScore captures the Expected Net Value calculation for a candidate action.
type CandidateActionScore struct {
	Action               string  `json:"action"`
	PredictedSuccessProb float64 `json:"predicted_success_prob"`
	ExpectedGrossRecover float64 `json:"expected_gross_recover"`
	ActionCost           float64 `json:"action_cost"`
	CustomerFrictionCost float64 `json:"customer_friction_cost"`
	RiskPenalty          float64 `json:"risk_penalty"`
	ExpectedNetValue     float64 `json:"expected_net_value"`
	PolicyAllowed        bool    `json:"policy_allowed"`
	VetoReason           string  `json:"veto_reason,omitempty"`
}

// DecisionRecord is produced by Decision Engine and consumed by Action Executor.
type DecisionRecord struct {
	TransactionID         string                 `json:"transaction_id"`
	MerchantID            string                 `json:"merchant_id"`
	RootCause             string                 `json:"root_cause"`
	Action                string                 `json:"action"`
	DecisionType          string                 `json:"decision_type"`
	ActionParams          *ActionParams          `json:"action_params,omitempty"`
	CandidateScores       []CandidateActionScore `json:"candidate_scores,omitempty"`
	ExpectedNetValue      float64                `json:"expected_net_value"`
	DecidedAt             time.Time              `json:"decided_at"`
	PlannedExecutionTime  time.Time              `json:"planned_execution_time"`
	CooldownSeconds       int                    `json:"cooldown_seconds"`
	StoppingRuleTriggered *string                `json:"stopping_rule_triggered"`
	PolicyVersion         string                 `json:"policy_version"`
	ModelID               string                 `json:"model_id"`
	DecisionReason        string                 `json:"decision_reason"`
}

// ActionResult is produced by Action Executor and recorded in Audit Store.
type ActionResult struct {
	TransactionID              string    `json:"transaction_id"`
	Action                     string    `json:"action"`
	ActionResult               string    `json:"action_result"`
	AmountRecovered            float64   `json:"amount_recovered"`
	ActionCost                 float64   `json:"action_cost"`
	NetRecovered               float64   `json:"net_recovered"`
	SimulatedSuccessProb       float64   `json:"simulated_success_prob"`
	PlannedExecutionTime       time.Time `json:"planned_execution_time"`
	ExecutedAt                 time.Time `json:"executed_at"`
	OperationalRecoveryLatency float64   `json:"operational_recovery_latency_sec"`
}

// AuditLogEntry is the canonical append-only record stored in the Audit Store.
type AuditLogEntry struct {
	AuditID       string          `json:"audit_id"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	TransactionID string          `json:"transaction_id"`
	MerchantID    string          `json:"merchant_id"`
	Stage         string          `json:"stage"`
	Payload       json.RawMessage `json:"payload"`
	Timestamp     time.Time       `json:"timestamp"`
}

// GroundTruth represents the synthetic dataset's true condition (held out for final evaluation only).
type GroundTruth struct {
	TransactionID        string  `json:"transaction_id"`
	TrueCause            string  `json:"true_cause"`
	WasRecoverable       bool    `json:"was_recoverable"`
	TrueRecoveryAmount   float64 `json:"true_recovery_amount"`
	BaseRecoverProb      float64 `json:"base_recover_prob"`
	NoiseApplied         bool    `json:"noise_applied"`
	GeneratedAt          string  `json:"generated_at,omitempty"`
	MerchantHealthStatus string  `json:"merchant_health_status,omitempty"`
}

// PolicyBenchmarkResult captures the economic metrics of a single recovery policy.
type PolicyBenchmarkResult struct {
	PolicyName         string  `json:"policy_name"`
	PolicyDescription  string  `json:"policy_description"`
	GrossRecoveredINR  float64 `json:"gross_recovered_inr"`
	GrossRecoveryRate  float64 `json:"gross_recovery_rate"`
	TotalActionCostINR float64 `json:"total_action_cost_inr"`
	FalseRetriesCount  int     `json:"false_retries_count"`
	FalseRetryCostINR  float64 `json:"false_retry_cost_inr"`
	NetRecoveredINR    float64 `json:"net_recovered_inr"`
	IncrementalLiftINR float64 `json:"incremental_lift_inr"`
	ComplianceVetoes   int     `json:"compliance_vetoes"`
}
