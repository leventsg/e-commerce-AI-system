package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultMaxResponseBytes int64 = 1 << 20

type Options struct {
	Client           *http.Client
	Headers          map[string]string
	MaxResponseBytes int64
}

type Option func(*Options)

func WithClient(client *http.Client) Option {
	return func(o *Options) {
		o.Client = client
	}
}

func WithHeader(key, value string) Option {
	return func(o *Options) {
		if o.Headers == nil {
			o.Headers = make(map[string]string)
		}
		o.Headers[key] = value
	}
}

func WithMaxResponseBytes(max int64) Option {
	return func(o *Options) {
		o.MaxResponseBytes = max
	}
}

type envelope[T any] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

func DoJSONEnvelope[T any](ctx context.Context, method, url string, body any, out *T, opts ...Option) error {
	options := Options{MaxResponseBytes: defaultMaxResponseBytes}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 5 * time.Second}
	}
	if options.MaxResponseBytes <= 0 {
		options.MaxResponseBytes = defaultMaxResponseBytes
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal json request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range options.Headers {
		req.Header.Set(key, value)
	}

	resp, err := options.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, options.MaxResponseBytes+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > options.MaxResponseBytes {
		return fmt.Errorf("http response too large: limit=%d", options.MaxResponseBytes)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("http status %d: %s", resp.StatusCode, truncateErrorBody(raw))
	}

	var env envelope[T]
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("decode json envelope: %w", err)
	}
	if env.Code != 0 {
		msg := strings.TrimSpace(env.Msg)
		if msg == "" {
			msg = "business error"
		}
		return fmt.Errorf("http business error code=%d msg=%s", env.Code, msg)
	}
	if out != nil {
		*out = env.Data
	}
	return nil
}

func truncateErrorBody(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 512 {
		return text[:512]
	}
	return text
}
