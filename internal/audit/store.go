package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"sync"

	"revenue-recovery-agent/internal/events"
)

// Store defines queryable, append-only audit trail operations.
type Store interface {
	Append(ctx context.Context, entry events.AuditLogEntry) error
	GetByTransaction(ctx context.Context, transactionID string) ([]events.AuditLogEntry, error)
	GetAll(ctx context.Context) ([]events.AuditLogEntry, error)
	Close() error
}

// JSONLStore implements Store using an append-only JSON Lines flat file.
type JSONLStore struct {
	filePath string
	file     *os.File
	mu       sync.RWMutex
}

// NewJSONLStore opens or creates an append-only audit file.
func NewJSONLStore(filePath string) (*JSONLStore, error) {
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &JSONLStore{
		filePath: filePath,
		file:     f,
	}, nil
}

// Append persists a new stage audit entry.
func (s *JSONLStore) Append(ctx context.Context, entry events.AuditLogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = s.file.Write(data)
	return err
}

// GetByTransaction queries all audit entries for a specific transaction.
func (s *JSONLStore) GetByTransaction(ctx context.Context, transactionID string) ([]events.AuditLogEntry, error) {
	all, err := s.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	var results []events.AuditLogEntry
	for _, entry := range all {
		if entry.TransactionID == transactionID {
			results = append(results, entry)
		}
	}
	return results, nil
}

// GetAll reads all audit log records.
func (s *JSONLStore) GetAll(ctx context.Context) ([]events.AuditLogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f, err := os.Open(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []events.AuditLogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry events.AuditLogEntry
		if err := json.Unmarshal(line, &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	return entries, scanner.Err()
}

// Close flushes and closes the audit log file.
func (s *JSONLStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}
