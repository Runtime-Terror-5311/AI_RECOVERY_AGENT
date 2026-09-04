package execute

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"revenue-recovery-agent/internal/events"
)

// Executor defines interface for executing recovery actions strictly on observed context.
type Executor interface {
	Execute(ctx context.Context, decision events.DecisionRecord, amount float64, method string) (events.ActionResult, error)
}

// ProbabilisticExecutor provides realistic execution with non-blocking rate limiting and zero ground-truth leakage.
type ProbabilisticExecutor struct {
	mu           sync.Mutex
	rateLimitRPS int
	lastExecuted time.Time
	rng          *rand.Rand
	costTable    map[string]float64
}

// NewProbabilisticExecutor instantiates an action executor.
func NewProbabilisticExecutor(rps int, seed int64) *ProbabilisticExecutor {
	if rps <= 0 {
		rps = 50
	}
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	return &ProbabilisticExecutor{
		rateLimitRPS: rps,
		rng:          rand.New(rand.NewSource(seed)),
		costTable: map[string]float64{
			events.ActionRetryImmediate:     2.50,
			events.ActionRetryDelayed:       2.50,
			events.ActionSendUpdateCardLink: 1.00,
			events.ActionEscalateHuman:      25.00,
			events.ActionNoAction:           0.00,
			events.ActionHaltMerchant:       0.00,
		},
	}
}

// waitForRateLimit ensures calls respect RPS without holding locks during wait.
func (e *ProbabilisticExecutor) waitForRateLimit(ctx context.Context) error {
	minInterval := time.Second / time.Duration(e.rateLimitRPS)

	e.mu.Lock()
	elapsed := time.Since(e.lastExecuted)
	var waitDuration time.Duration
	if elapsed < minInterval {
		waitDuration = minInterval - elapsed
		e.lastExecuted = time.Now().Add(waitDuration)
	} else {
		e.lastExecuted = time.Now()
	}
	e.mu.Unlock()

	if waitDuration > 0 {
		select {
		case <-time.After(waitDuration):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Execute carries out the recovery action based solely on observed transaction attributes (zero ground-truth leakage).
func (e *ProbabilisticExecutor) Execute(ctx context.Context, decision events.DecisionRecord, amount float64, method string) (events.ActionResult, error) {
	if err := e.waitForRateLimit(ctx); err != nil {
		return events.ActionResult{}, err
	}

	now := time.Now().UTC()
	actionCost := e.costTable[decision.Action]

	attempt := 1
	if decision.ActionParams != nil && decision.ActionParams.RetryCount > 0 {
		attempt = decision.ActionParams.RetryCount
	}

	// Environment settlement dynamics (real-world probability of settlement given action & context)
	var settlementProb float64 = 0.0

	switch decision.Action {
	case events.ActionRetryImmediate:
		if decision.RootCause == events.RootCauseGatewayTimeout || decision.RootCause == events.RootCauseNetworkError {
			settlementProb = 0.76 / float64(attempt)
		} else {
			settlementProb = 0.10 // Immediate retry on other causes rarely clears
		}

	case events.ActionRetryDelayed:
		switch decision.RootCause {
		case events.RootCauseIssuerDecline:
			settlementProb = 0.68 / float64(attempt)
		case events.RootCauseInsufficientFunds:
			settlementProb = 0.62 / float64(attempt)
		case events.RootCauseGatewayTimeout, events.RootCauseNetworkError:
			settlementProb = 0.72 / float64(attempt)
		default:
			settlementProb = 0.20 / float64(attempt)
		}

	case events.ActionSendUpdateCardLink:
		if method == events.PaymentMethodCard || method == events.PaymentMethodEmandate {
			settlementProb = 0.45 // 45% customer link conversion
		} else {
			settlementProb = 0.00
		}

	case events.ActionEscalateHuman:
		settlementProb = 0.25 // Manual ops review recovers 25% of edge cases

	case events.ActionHaltMerchant, events.ActionNoAction:
		settlementProb = 0.00

	default:
		settlementProb = 0.00
	}

	// Stochastic outcome resolution
	e.mu.Lock()
	roll := e.rng.Float64()
	e.mu.Unlock()

	var outcome string
	var recoveredAmount float64

	if decision.Action == events.ActionHaltMerchant || decision.Action == events.ActionNoAction {
		outcome = events.ActionResultSkipped
		recoveredAmount = 0.0
	} else if roll < settlementProb {
		outcome = events.ActionResultSuccess
		recoveredAmount = amount
	} else {
		outcome = events.ActionResultFailed
		recoveredAmount = 0.0
	}

	netRecovered := recoveredAmount - actionCost
	operationalLatency := float64(decision.CooldownSeconds)

	return events.ActionResult{
		TransactionID:              decision.TransactionID,
		Action:                     decision.Action,
		ActionResult:               outcome,
		AmountRecovered:            recoveredAmount,
		ActionCost:                 actionCost,
		NetRecovered:               netRecovered,
		SimulatedSuccessProb:       settlementProb,
		PlannedExecutionTime:       decision.PlannedExecutionTime,
		ExecutedAt:                 now,
		OperationalRecoveryLatency: operationalLatency,
	}, nil
}
