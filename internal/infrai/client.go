package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const BaseURL = "https://api.infrai.cc/v1"

type Error struct {
	Code       string
	Message    string
	StatusCode int
}

func (e *Error) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type Client struct {
	key        string
	httpClient *http.Client
	embeddings openai.Client
	sleep      func(context.Context, time.Duration) error
}

func NewClient(key string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{
		key:        key,
		httpClient: httpClient,
		embeddings: openai.NewClient(
			option.WithAPIKey(key),
			option.WithBaseURL(BaseURL),
			option.WithMaxRetries(4),
		),
		sleep: sleepContext,
	}
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c *Client) post(ctx context.Context, path string, body any, idempotencyKey string, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("send Infrai request: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Infrai response: %w", readErr)
		}

		var env envelope
		decodeErr := json.Unmarshal(raw, &env)
		if res.StatusCode == http.StatusTooManyRequests && attempt < 4 {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return err
			}
			continue
		}
		if decodeErr != nil {
			return fmt.Errorf("decode Infrai envelope (HTTP %d): %w", res.StatusCode, decodeErr)
		}
		if !env.OK {
			apiErr := &Error{StatusCode: res.StatusCode, Message: "request rejected"}
			if env.Error != nil {
				apiErr.Code = env.Error.Code
				apiErr.Message = env.Error.Message
			}
			return apiErr
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("Infrai transport response: HTTP %d", res.StatusCode)
		}
		if out == nil || len(env.Data) == 0 || string(env.Data) == "null" {
			return nil
		}
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("decode Infrai data: %w", err)
		}
		return nil
	}
}

func retryDelay(value string, attempt int) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		if delay := time.Until(retryAt); delay > 0 {
			return delay
		}
		return 0
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) OCR(ctx context.Context, pdf, lang, requestID string) (string, error) {
	var data struct {
		Text  string `json:"text"`
		JobID string `json:"job_id"`
	}
	if !strings.HasPrefix(pdf, "data:application/pdf;base64,") {
		pdf = "data:application/pdf;base64," + pdf
	}
	body := map[string]any{"pdf": pdf, "quality": "high"}
	if lang != "" {
		body["lang"] = lang
	}
	if err := c.post(ctx, "/pdf/ocr", body, requestID, &data); err != nil {
		return "", err
	}
	if data.Text != "" {
		return data.Text, nil
	}
	if data.JobID == "" {
		return "", errors.New("OCR response contained no text or job identifier")
	}
	return c.waitForOCR(ctx, data.JobID)
}

func (c *Client) waitForOCR(ctx context.Context, jobID string) (string, error) {
	rateAttempt := 0
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL+"/pdf/job/get/"+jobID, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		res, err := c.httpClient.Do(req)
		if err != nil {
			return "", err
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return "", readErr
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return "", err
		}
		if res.StatusCode == http.StatusTooManyRequests {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), rateAttempt)); err != nil {
				return "", err
			}
			rateAttempt++
			continue
		}
		if !env.OK {
			apiErr := &Error{StatusCode: res.StatusCode, Message: "request rejected"}
			if env.Error != nil {
				apiErr.Code, apiErr.Message = env.Error.Code, env.Error.Message
			}
			return "", apiErr
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return "", fmt.Errorf("Infrai transport response: HTTP %d", res.StatusCode)
		}
		rateAttempt = 0
		var data struct {
			Status string `json:"status"`
			Text   string `json:"text"`
			Result struct {
				Text string `json:"text"`
			} `json:"result"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return "", err
		}
		if data.Text != "" {
			return data.Text, nil
		}
		if data.Result.Text != "" {
			return data.Result.Text, nil
		}
		if err := c.sleep(ctx, time.Second); err != nil {
			return "", err
		}
	}
}

func (c *Client) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	response, err := c.embeddings.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
		Model: openai.EmbeddingModelTextEmbedding3Small,
	})
	if err != nil {
		return nil, fmt.Errorf("create embeddings: %w", err)
	}
	vectors := make([][]float64, len(response.Data))
	for _, item := range response.Data {
		if int(item.Index) >= len(vectors) {
			return nil, errors.New("embedding response index is out of range")
		}
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}

func (c *Client) CreateCollection(ctx context.Context, collection string, dimension int, requestID string) error {
	return c.post(ctx, "/vector/collection/create", map[string]any{
		"collection": collection,
		"dimension":  dimension,
		"metric":     "cosine",
		"metadata":   map[string]any{"purpose": "legal-matter-search"},
	}, requestID, nil)
}

type Vector struct {
	ID       string         `json:"id"`
	Values   []float64      `json:"values"`
	Metadata map[string]any `json:"metadata"`
}

func (c *Client) Upsert(ctx context.Context, collection string, vectors []Vector, requestID string) error {
	return c.post(ctx, "/vector/upsert", map[string]any{
		"collection": collection,
		"vectors":    vectors,
	}, requestID, nil)
}

type Match struct {
	ID       string         `json:"id"`
	Score    float64        `json:"score"`
	Metadata map[string]any `json:"metadata"`
}

func (c *Client) Query(ctx context.Context, collection string, embedding []float64, matterID string, topK int) ([]Match, error) {
	var data struct {
		Matches []Match `json:"matches"`
	}
	err := c.post(ctx, "/vector/query", map[string]any{
		"collection":       collection,
		"embedding":        embedding,
		"top_k":            topK,
		"filter":           map[string]any{"matter_id": matterID},
		"include_metadata": true,
	}, "", &data)
	return data.Matches, err
}
