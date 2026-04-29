package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"osagentmvp/internal/models"
)

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/settings/gateway", s.handleGatewaySettings)
	mux.HandleFunc("/api/settings/operator", s.handleOperatorSettings)
	mux.HandleFunc("/api/settings/policy", s.handlePolicySettings)
	mux.HandleFunc("/api/knowledge", s.handleKnowledge)
	mux.HandleFunc("/api/hosts", s.handleHosts)
	mux.HandleFunc("/api/automations", s.handleAutomations)
	mux.HandleFunc("/api/automations/", s.handleAutomationRoutes)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionRoutes)
	mux.HandleFunc("/api/runs", s.handleRuns)
	mux.HandleFunc("/api/runs/", s.handleRunRoutes)
	mux.HandleFunc("/api/approvals", s.handleApprovals)
	mux.HandleFunc("/api/approvals/", s.handleApprovalRoutes)
	mux.HandleFunc("/api/events/stream", s.streamAllEvents)
}

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	health, err := s.HealthSnapshot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, health)
}

func (s *Service) handleGatewaySettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.GatewayConfigView())
	case http.MethodPut:
		var request models.GatewayConfig
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		updated, err := s.UpdateGatewayConfig(request)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handlePolicySettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		config, err := s.PolicyConfig()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, config)
	case http.MethodPut:
		var request models.PolicyConfig
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		updated, err := s.UpdatePolicyConfig(request, "api")
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleOperatorSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profile, err := s.OperatorProfile()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, profile)
	case http.MethodPut:
		var request models.OperatorProfile
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		updated, err := s.UpdateOperatorProfile(request, "api")
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleKnowledge(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.ListKnowledge()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var item models.KnowledgeItem
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		saved, err := s.SaveKnowledge(item)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, saved)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.ListHosts()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var host models.Host
		if err := json.NewDecoder(r.Body).Decode(&host); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		saved, err := s.UpsertHost(host)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, saved)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.ListRuns()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		items = limitItems(items, parsePositiveInt(r, "limit", 0))
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var request RunRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		run, err := s.CreateRun(r.Context(), request)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusAccepted, run)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.ListSessions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	items = limitItems(items, parsePositiveInt(r, "limit", 0))
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Service) handleSessionRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/")
	if strings.HasSuffix(path, "/mode") {
		id := strings.TrimSuffix(path, "/mode")
		if id == "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var mode models.SessionMode
		if err := json.NewDecoder(r.Body).Decode(&mode); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		session, err := s.UpdateSessionMode(id, mode)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, session)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if path == "" {
		http.NotFound(w, r)
		return
	}
	detail, found, err := s.GetSessionDetailWithOptions(path, SessionDetailOptions{
		TurnLimit:  parsePositiveInt(r, "turn_limit", 0),
		EventLimit: parsePositiveInt(r, "events_limit", 0),
		Compact:    parseBoolQuery(r, "compact"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Service) handleAutomations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.ListAutomations()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost, http.MethodPut:
		var rule models.AutomationRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		saved, err := s.SaveAutomation(rule)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, saved)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleAutomationRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/automations/"), "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")
	automationID := parts[0]
	if len(parts) == 1 {
		s.handleAutomationItem(w, r, automationID)
		return
	}
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	switch parts[1] {
	case "sample":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		sample, err := s.SampleAutomation(r.Context(), automationID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, sample)
	case "test":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			Force bool `json:"force"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&request)
		}
		result, err := s.TestAutomation(r.Context(), automationID, request.Force)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	default:
		http.NotFound(w, r)
	}
}

func (s *Service) handleAutomationItem(w http.ResponseWriter, r *http.Request, automationID string) {
	switch r.Method {
	case http.MethodGet:
		item, found, err := s.GetAutomation(automationID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if !found {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var rule models.AutomationRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		rule.ID = automationID
		saved, err := s.SaveAutomation(rule)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, saved)
	case http.MethodDelete:
		if err := s.DeleteAutomation(automationID); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": automationID})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleRunRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/runs/")
	if strings.HasSuffix(path, "/events") {
		runID := strings.TrimSuffix(path, "/events")
		items, err := s.ListRunEvents(runID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	if strings.HasSuffix(path, "/events/stream") {
		s.streamRunEvents(w, r, strings.TrimSuffix(path, "/events/stream"))
		return
	}
	run, found, err := s.GetRun(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Service) handleApprovals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.ListApprovals()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if parseBoolQuery(r, "pending") {
		filtered := items[:0]
		for _, item := range items {
			if item.Decision == "" {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	items = limitItems(items, parsePositiveInt(r, "limit", 0))
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Service) handleApprovalRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/approvals/")
	if !strings.HasSuffix(path, "/resolve") {
		http.NotFound(w, r)
		return
	}
	approvalID := strings.TrimSuffix(path, "/resolve")
	var request struct {
		Decision string `json:"decision"`
		Actor    string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	run, err := s.ResolveApproval(approvalID, request.Decision, request.Actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func parsePositiveInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func parseBoolQuery(r *http.Request, key string) bool {
	raw := strings.TrimSpace(strings.ToLower(r.URL.Query().Get(key)))
	return raw == "1" || raw == "true" || raw == "yes"
}

func limitItems[T any](items []T, limit int) []T {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func (s *Service) streamRunEvents(w http.ResponseWriter, r *http.Request, runID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, context.Canceled)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	items, _ := s.ListRunEvents(runID)
	for _, item := range items {
		writeSSE(w, item)
	}
	flusher.Flush()

	ch, unsubscribe := s.SubscribeRun(runID)
	defer unsubscribe()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			writeSSE(w, event)
			flusher.Flush()
		case <-ticker.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

func (s *Service) streamAllEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, context.Canceled)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsubscribe := s.SubscribeAllEvents()
	defer unsubscribe()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			writeSSE(w, event)
			flusher.Flush()
		case <-ticker.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func writeSSE(w http.ResponseWriter, event models.Event) {
	data, _ := json.Marshal(event)
	_, _ = w.Write([]byte("id: "))
	_, _ = w.Write([]byte(event.ID))
	_, _ = w.Write([]byte("\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n\n"))
}
