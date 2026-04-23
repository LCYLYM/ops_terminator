package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"osagentmvp/internal/models"
)

type Client struct {
	httpClient *http.Client
	mu         sync.RWMutex
	baseURL    string
	apiKey     string
	model      string
}

type chatCompletionRequest struct {
	Model       string                  `json:"model"`
	Messages    []models.ChatMessage    `json:"messages"`
	Tools       []models.ToolDefinition `json:"tools,omitempty"`
	ToolChoice  string                  `json:"tool_choice,omitempty"`
	Stream      bool                    `json:"stream"`
	MaxTokens   int                     `json:"max_tokens,omitempty"`
	Temperature float64                 `json:"temperature,omitempty"`
}

type streamChunk struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []streamChoice `json:"choices"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type streamDelta struct {
	Content   string          `json:"content"`
	ToolCalls []toolCallDelta `json:"tool_calls"`
}

type toolCallDelta struct {
	Index    int                   `json:"index"`
	ID       string                `json:"id"`
	Type     string                `json:"type"`
	Function toolFunctionCallDelta `json:"function"`
}

type toolFunctionCallDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type apiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

type toolCallAccumulator struct {
	ID        string
	Type      string
	Name      string
	Arguments strings.Builder
}

func NewClient(baseURL, apiKey, model string, timeout time.Duration) *Client {
	client := &Client{
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
	client.UpdateConfig(baseURL, apiKey, model)
	return client
}

func (c *Client) UpdateConfig(baseURL, apiKey, model string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	c.apiKey = strings.TrimSpace(apiKey)
	c.model = strings.TrimSpace(model)
}

func (c *Client) SnapshotConfig() (baseURL, apiKey, model string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL, c.apiKey, c.model
}

func (c *Client) StreamChatCompletion(ctx context.Context, messages []models.ChatMessage, tools []models.ToolDefinition, onText func(string)) (*models.AssistantResponse, error) {
	baseURL, apiKey, model := c.SnapshotConfig()
	requestBody := chatCompletionRequest{
		Model:       model,
		Messages:    messages,
		Tools:       tools,
		ToolChoice:  "auto",
		Stream:      true,
		MaxTokens:   4096,
		Temperature: 0.2,
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsURL(baseURL), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, c.readAPIError(resp)
	}

	result := &models.AssistantResponse{}
	var content strings.Builder
	accumulators := map[int]*toolCallAccumulator{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return nil, fmt.Errorf("decode stream chunk: %w", err)
		}
		if result.ID == "" {
			result.ID = chunk.ID
			result.Model = chunk.Model
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				onText(choice.Delta.Content)
				content.WriteString(choice.Delta.Content)
			}
			for _, deltaToolCall := range choice.Delta.ToolCalls {
				acc := accumulators[deltaToolCall.Index]
				if acc == nil {
					acc = &toolCallAccumulator{}
					accumulators[deltaToolCall.Index] = acc
				}
				if deltaToolCall.ID != "" {
					acc.ID = deltaToolCall.ID
				}
				if deltaToolCall.Type != "" {
					acc.Type = deltaToolCall.Type
				}
				if deltaToolCall.Function.Name != "" {
					acc.Name = deltaToolCall.Function.Name
				}
				if deltaToolCall.Function.Arguments != "" {
					acc.Arguments.WriteString(deltaToolCall.Function.Arguments)
				}
			}
			if choice.FinishReason != nil {
				result.FinishReason = *choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}

	result.Content = content.String()
	for index := 0; index < len(accumulators); index++ {
		acc, ok := accumulators[index]
		if !ok {
			continue
		}
		result.ToolCalls = append(result.ToolCalls, models.ToolCall{
			ID:   acc.ID,
			Type: firstNonEmpty(acc.Type, "function"),
			Function: models.ToolFunctionCall{
				Name:      acc.Name,
				Arguments: acc.Arguments.String(),
			},
		})
	}
	return result, nil
}

func chatCompletionsURL(baseURL string) string {
	if strings.HasSuffix(baseURL, "/openai") {
		return baseURL + "/v1/chat/completions"
	}
	return baseURL + "/openai/v1/chat/completions"
}

func (c *Client) readAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	var apiErr apiErrorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		return fmt.Errorf("api error: status=%d type=%s code=%s message=%s", resp.StatusCode, apiErr.Error.Type, apiErr.Error.Code, apiErr.Error.Message)
	}
	return fmt.Errorf("api error: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
