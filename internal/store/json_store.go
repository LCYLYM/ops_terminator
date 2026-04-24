package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"osagentmvp/internal/models"
)

type JSONStore struct {
	root string
	mu   sync.Mutex
}

func NewJSONStore(root string) (*JSONStore, error) {
	paths := []string{
		filepath.Join(root, "hosts"),
		filepath.Join(root, "sessions"),
		filepath.Join(root, "turns"),
		filepath.Join(root, "runs"),
		filepath.Join(root, "approvals"),
		filepath.Join(root, "automations"),
		filepath.Join(root, "events"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", path, err)
		}
	}
	return &JSONStore{root: root}, nil
}

func (s *JSONStore) ListHosts() ([]models.Host, error) { return listObjects[models.Host](s, "hosts") }
func (s *JSONStore) GetHost(id string) (models.Host, bool, error) {
	return getObject[models.Host](s, "hosts", id)
}
func (s *JSONStore) SaveHost(host models.Host) error { return saveObject(s, "hosts", host.ID, host) }

func (s *JSONStore) GetGatewayConfig() (models.GatewayConfig, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero models.GatewayConfig
	path := filepath.Join(s.root, "gateway_config.json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, false, nil
		}
		return zero, false, fmt.Errorf("read %s: %w", path, err)
	}
	var item models.GatewayConfig
	if err := json.Unmarshal(bytes, &item); err != nil {
		return zero, false, fmt.Errorf("decode %s: %w", path, err)
	}
	return item, true, nil
}

func (s *JSONStore) SaveGatewayConfig(config models.GatewayConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.root, "gateway_config.json")
	bytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal gateway config: %w", err)
	}
	return os.WriteFile(path, bytes, 0o644)
}

func (s *JSONStore) ListSessions() ([]models.Session, error) {
	return listObjects[models.Session](s, "sessions")
}
func (s *JSONStore) GetSession(id string) (models.Session, bool, error) {
	return getObject[models.Session](s, "sessions", id)
}
func (s *JSONStore) SaveSession(session models.Session) error {
	return saveObject(s, "sessions", session.ID, session)
}

func (s *JSONStore) ListTurns() ([]models.Turn, error) { return listObjects[models.Turn](s, "turns") }
func (s *JSONStore) GetTurn(id string) (models.Turn, bool, error) {
	return getObject[models.Turn](s, "turns", id)
}
func (s *JSONStore) SaveTurn(turn models.Turn) error { return saveObject(s, "turns", turn.ID, turn) }

func (s *JSONStore) ListRuns() ([]models.Run, error) { return listObjects[models.Run](s, "runs") }
func (s *JSONStore) GetRun(id string) (models.Run, bool, error) {
	return getObject[models.Run](s, "runs", id)
}
func (s *JSONStore) SaveRun(run models.Run) error { return saveObject(s, "runs", run.ID, run) }

func (s *JSONStore) ListApprovals() ([]models.Approval, error) {
	return listObjects[models.Approval](s, "approvals")
}
func (s *JSONStore) GetApproval(id string) (models.Approval, bool, error) {
	return getObject[models.Approval](s, "approvals", id)
}
func (s *JSONStore) SaveApproval(approval models.Approval) error {
	return saveObject(s, "approvals", approval.ID, approval)
}

func (s *JSONStore) ListAutomations() ([]models.AutomationRule, error) {
	return listObjects[models.AutomationRule](s, "automations")
}
func (s *JSONStore) GetAutomation(id string) (models.AutomationRule, bool, error) {
	return getObject[models.AutomationRule](s, "automations", id)
}
func (s *JSONStore) SaveAutomation(rule models.AutomationRule) error {
	return saveObject(s, "automations", rule.ID, rule)
}
func (s *JSONStore) DeleteAutomation(id string) error {
	return deleteObject(s, "automations", id)
}

func (s *JSONStore) AppendEvent(event models.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.root, "events", event.RunID+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open events file: %w", err)
	}
	defer file.Close()

	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	_, err = file.Write(append(encoded, '\n'))
	return err
}

func (s *JSONStore) ListEventsByRun(runID string) ([]models.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.root, "events", runID+".jsonl")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open events file: %w", err)
	}
	defer file.Close()

	var items []models.Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var item models.Event
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		items = append(items, item)
	}
	return items, scanner.Err()
}

func (s *JSONStore) AppendAudit(entry models.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.root, "audit.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open audit file: %w", err)
	}
	defer file.Close()

	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal audit: %w", err)
	}
	_, err = file.Write(append(encoded, '\n'))
	return err
}

func listObjects[T any](s *JSONStore, dir string) ([]T, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pattern := filepath.Join(s.root, dir, "*.json")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", pattern, err)
	}
	sort.Strings(paths)

	items := make([]T, 0, len(paths))
	for _, path := range paths {
		bytes, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var item T
		if err := json.Unmarshal(bytes, &item); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		items = append(items, item)
	}
	return items, nil
}

func getObject[T any](s *JSONStore, dir, id string) (T, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero T
	path := filepath.Join(s.root, dir, id+".json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, false, nil
		}
		return zero, false, fmt.Errorf("read %s: %w", path, err)
	}
	var item T
	if err := json.Unmarshal(bytes, &item); err != nil {
		return zero, false, fmt.Errorf("decode %s: %w", path, err)
	}
	return item, true, nil
}

func saveObject(s *JSONStore, dir, id string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.root, dir, id+".json")
	bytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s/%s: %w", dir, id, err)
	}
	return os.WriteFile(path, bytes, 0o644)
}

func deleteObject(s *JSONStore, dir, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.root, dir, id+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete %s/%s: %w", dir, id, err)
	}
	return nil
}
