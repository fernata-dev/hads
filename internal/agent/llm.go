package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client talks to any OpenAI-compatible endpoint; OpenRouter by default.
type Client struct {
	BaseURL string
	Key     string
	Model   string
	HTTP    *http.Client
}

func NewClient(base, key, model string) *Client {
	return &Client{BaseURL: strings.TrimRight(base, "/"), Key: key, Model: model, HTTP: &http.Client{Timeout: 120 * time.Second}}
}

type chatReq struct {
	Model          string          `json:"model"`
	Messages       []Msg           `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens"`
	ResponseFormat *map[string]any `json:"response_format,omitempty"`
}

type chatResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

type HTTPError struct {
	Status int
	Msg    string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Msg) }

func (c *Client) Chat(ctx context.Context, msgs []Msg, jsonMode bool, maxTokens int) (string, error) {
	out, err := c.chat(ctx, msgs, jsonMode, maxTokens)
	var he *HTTPError
	if jsonMode && errors.As(err, &he) && he.Status == 400 {
		return c.chat(ctx, msgs, false, maxTokens) // model doesn't support response_format
	}
	return out, err
}

func (c *Client) chat(ctx context.Context, msgs []Msg, jsonMode bool, maxTokens int) (string, error) {
	body := chatReq{Model: c.Model, Messages: msgs, Temperature: 0.1, MaxTokens: maxTokens}
	if jsonMode {
		rf := map[string]any{"type": "json_object"}
		body.ResponseFormat = &rf
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "Hads CLI")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var cr chatResp
	_ = json.Unmarshal(raw, &cr)
	if resp.StatusCode != 200 {
		msg := strings.TrimSpace(string(raw))
		if cr.Error != nil && cr.Error.Message != "" {
			msg = cr.Error.Message
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return "", &HTTPError{Status: resp.StatusCode, Msg: msg}
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", errors.New(cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", errors.New("empty response from model")
	}
	return cr.Choices[0].Message.Content, nil
}
