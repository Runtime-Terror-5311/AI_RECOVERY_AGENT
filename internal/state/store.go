package state

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"revenue-recovery-agent/internal/events"
)

// TransactionHistory tracks an individual transaction across multiple attempts and stages.
type TransactionHistory struct {
	TransactionID      string    `json:"transaction_id"`
	MerchantID         string    `json:"merchant_id"`
	CustomerID         string    `json:"customer_id"`
	AttemptCount       int       `json:"attempt_count"`
	FirstFailedAt      time.Time `json:"first_failed_at"`
	LastAttemptAt      time.Time `json:"last_attempt_at"`
	CurrentStatus      string    `json:"current_status"` // failed, recovered, escalated, halted
	PriorActions       []string  `json:"prior_actions"`
	CumulativeCost     float64   `json:"cumulative_cost"`
	TotalRecovered     float64   `json:"total_recovered"`
	LastRootCause      string    `json:"last_root_cause"`
	LastClassification events.ClassificationRecord `json:"last_classification"`
}

// MerchantEvent records a timestamped failure/success for rolling-window calculations.
type MerchantEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	IsFailure  bool      `json:"is_failure"`
	Amount     float64   `json:"amount"`
}

// StateStore defines methods to track transaction history and merchant rolling windows.
type StateStore interface {
	GetTransaction(ctx context.Context, txID string) (*TransactionHistory, error)
	RecordTransactionEvent(ctx context.Context, raw events.RawPaymentEvent) (*TransactionHistory, error)
	RecordActionResult(ctx context.Context, res events.ActionResult) error
	GetMerchantFailureRate(ctx context.Context, merchantID string, windowDuration time.Duration) (float64, int, error)
	RecordMerchantEvent(ctx context.Context, merchantID string, isFailure bool, amount float64, ts time.Time) error
	Close() error
}

// MemoryStateStore provides a thread-safe in-memory state store with optional JSON snapshotting.
type MemoryStateStore struct {
	mu             sync.RWMutex
	transactions   map[string]*TransactionHistory
	merchantEvents map[string][]MerchantEvent
	snapshotPath   string
}

// NewMemoryStateStore creates a new state store instance.
func NewMemoryStateStore(snapshotPath string) (*MemoryStateStore, error) {
	store := &MemoryStateStore{
		transactions:   make(map[string]*TransactionHistory),
		merchantEvents: make(map[string][]MerchantEvent),
		snapshotPath:   snapshotPath,
	}

	if snapshotPath != "" {
		if data, err := os.ReadFile(snapshotPath); err == nil && len(data) > 0 {
			var snapshot struct {
				Transactions   map[string]*TransactionHistory `json:"transactions"`
				MerchantEvents map[string][]MerchantEvent     `json:"merchant_events"`
			}
			if err := json.Unmarshal(data, &snapshot); err == nil {
				if snapshot.Transactions != nil {
					store.transactions = snapshot.Transactions
				}
				if snapshot.MerchantEvents != nil {
					store.merchantEvents = snapshot.MerchantEvents
				}
			}
		}
	}

	return store, nil
}

// GetTransaction retrieves the history of a transaction.
func (s *MemoryStateStore) GetTransaction(ctx context.Context, txID string) (*TransactionHistory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if hist, ok := s.transactions[txID]; ok {
		// Return copy
		copyHist := *hist
		return &copyHist, nil
	}
	return nil, nil
}

// RecordTransactionEvent updates or creates the transaction record upon receiving an event.
func (s *MemoryStateStore) RecordTransactionEvent(ctx context.Context, raw events.RawPaymentEvent) (*TransactionHistory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hist, exists := s.transactions[raw.TransactionID]
	if !exists {
		hist = &TransactionHistory{
			TransactionID: raw.TransactionID,
			MerchantID:    raw.MerchantID,
			CustomerID:    raw.CustomerID,
			AttemptCount:  raw.AttemptNumber,
			FirstFailedAt: raw.Timestamp,
			LastAttemptAt: raw.Timestamp,
			CurrentStatus: "failed",
			PriorActions:  make([]string, 0),
		}
		s.transactions[raw.TransactionID] = hist
	} else {
		if raw.AttemptNumber > hist.AttemptCount {
			hist.AttemptCount = raw.AttemptNumber
		}
		hist.LastAttemptAt = raw.Timestamp
	}

	// Also record merchant event
	isFail := raw.EventType == events.EventTypePaymentFailed || raw.EventType == events.EventTypePaymentTimeout
	s.appendMerchantEvent(raw.MerchantID, isFail, raw.Amount, raw.Timestamp)

	return hist, nil
}

// RecordActionResult updates transaction status and financial tracking after an action executes.
func (s *MemoryStateStore) RecordActionResult(ctx context.Context, res events.ActionResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if hist, ok := s.transactions[res.TransactionID]; ok {
		hist.PriorActions = append(hist.PriorActions, res.Action)
		hist.CumulativeCost += res.ActionCost
		hist.TotalRecovered += res.AmountRecovered
		if res.AmountRecovered > 0 {
			hist.CurrentStatus = "recovered"
		} else if res.Action == events.ActionEscalateHuman {
			hist.CurrentStatus = "escalated"
		} else if res.Action == events.ActionHaltMerchant {
			hist.CurrentStatus = "halted"
		}
	}
	return nil
}

// RecordMerchantEvent logs an event into the merchant rolling window.
func (s *MemoryStateStore) RecordMerchantEvent(ctx context.Context, merchantID string, isFailure bool, amount float64, ts time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendMerchantEvent(merchantID, isFailure, amount, ts)
	return nil
}

func (s *MemoryStateStore) appendMerchantEvent(merchantID string, isFailure bool, amount float64, ts time.Time) {
	if merchantID == "" {
		merchantID = "default_merchant"
	}
	eventsList := s.merchantEvents[merchantID]
	eventsList = append(eventsList, MerchantEvent{
		Timestamp: ts,
		IsFailure: isFailure,
		Amount:    amount,
	})
	s.merchantEvents[merchantID] = eventsList
}

// GetMerchantFailureRate calculates the actual rolling failure rate in a sliding time window.
func (s *MemoryStateStore) GetMerchantFailureRate(ctx context.Context, merchantID string, windowDuration time.Duration) (float64, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if merchantID == "" {
		merchantID = "default_merchant"
	}

	eventsList, ok := s.merchantEvents[merchantID]
	if !ok || len(eventsList) == 0 {
		return 0.0, 0, nil
	}

	// Calculate cutoff from the most recent event timestamp or now
	latestTime := eventsList[len(eventsList)-1].Timestamp
	cutoff := latestTime.Add(-windowDuration)

	var totalCount int
	var failureCount int

	for _, ev := range eventsList {
		if ev.Timestamp.After(cutoff) || ev.Timestamp.Equal(cutoff) {
			totalCount++
			if ev.IsFailure {
				failureCount++
			}
		}
	}

	if totalCount == 0 {
		return 0.0, 0, nil
	}

	failureRate := float64(failureCount) / float64(totalCount)
	return failureRate, totalCount, nil
}

// Close saves the snapshot to file if configured.
func (s *MemoryStateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshotPath != "" {
		data, err := json.MarshalIndent(struct {
			Transactions   map[string]*TransactionHistory `json:"transactions"`
			MerchantEvents map[string][]MerchantEvent     `json:"merchant_events"`
		}{
			Transactions:   s.transactions,
			MerchantEvents: s.merchantEvents,
		}, "", "  ")
		if err == nil {
			_ = os.WriteFile(s.snapshotPath, data, 0644)
		}
	}
	return nil
}
