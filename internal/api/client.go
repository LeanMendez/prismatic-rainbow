// Package api is a small hand-written client for the Bitbucket Cloud REST
// API 2.0: authentication, pagination, retries and error decoding shared
// by the per-resource files of this package.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the Bitbucket Cloud API root.
const DefaultBaseURL = "https://api.bitbucket.org/2.0"

// Client talks to Bitbucket Cloud. With Email set it uses HTTP Basic
// (Atlassian account email + API token); with Email empty it sends the
// token as Bearer (workspace/project/repository access tokens, or an API
// token used without email).
type Client struct {
	HTTP      *http.Client
	BaseURL   string
	Email     string
	Token     string
	UserAgent string

	// MaxRetries bounds retries of idempotent requests on 429/502/503/504.
	MaxRetries int
	// Sleep is swapped in tests to avoid real waits.
	Sleep func(time.Duration)
}

// NewClient builds a Client. baseURL "" means DefaultBaseURL.
func NewClient(baseURL, email, token, version string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		HTTP:       &http.Client{Timeout: 60 * time.Second},
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Email:      email,
		Token:      token,
		UserAgent:  "prismatic-rainbow/" + version,
		MaxRetries: 3,
		Sleep:      time.Sleep,
	}
}

// AuthMode reports "basic", "bearer" or "none".
func (c *Client) AuthMode() string {
	switch {
	case c.Token == "":
		return "none"
	case c.Email != "":
		return "basic"
	default:
		return "bearer"
	}
}

// Error is a non-2xx Bitbucket response.
type Error struct {
	StatusCode int
	Message    string
	// RequiredScopes is filled from error.detail.required on 403 scope
	// errors, so callers can tell the user exactly which scope is missing.
	RequiredScopes []string
	RetryAfter     time.Duration
}

func (e *Error) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Sprintf("HTTP 401: %s (credenciales inválidas o vencidas; revisá 'prismatic-rainbow auth status')", msg)
	case http.StatusForbidden:
		if len(e.RequiredScopes) > 0 {
			return fmt.Sprintf("HTTP 403: %s (al token le faltan scopes: %s)", msg, strings.Join(e.RequiredScopes, ", "))
		}
		return fmt.Sprintf("HTTP 403: %s (sin permisos: revisá acceso al repo y scopes del token)", msg)
	case http.StatusTooManyRequests:
		return fmt.Sprintf("HTTP 429: %s (rate limit de Bitbucket; reintentá más tarde)", msg)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, msg)
}

// Code is a stable machine-readable category for JSON error output.
func (e *Error) Code() string {
	switch e.StatusCode {
	case 401:
		return "unauthenticated"
	case 403:
		if len(e.RequiredScopes) > 0 {
			return "missing_scopes"
		}
		return "forbidden"
	case 404:
		return "not_found"
	case 409:
		return "conflict"
	case 429:
		return "rate_limited"
	}
	if e.StatusCode >= 500 {
		return "server_error"
	}
	return "bad_request"
}

// IsNotFound reports whether err is a Bitbucket 404.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.StatusCode == http.StatusNotFound
}

type errorBody struct {
	Error struct {
		Message string          `json:"message"`
		Detail  json.RawMessage `json:"detail"`
		Fields  json.RawMessage `json:"fields"`
	} `json:"error"`
}

func decodeError(resp *http.Response, body []byte) *Error {
	e := &Error{StatusCode: resp.StatusCode}
	var parsed errorBody
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		e.Message = parsed.Error.Message
		var detail struct {
			Required []string `json:"required"`
		}
		if json.Unmarshal(parsed.Error.Detail, &detail) == nil {
			e.RequiredScopes = detail.Required
		} else {
			var s string
			if json.Unmarshal(parsed.Error.Detail, &s) == nil && s != "" {
				e.Message += ": " + s
			}
		}
		if len(parsed.Error.Fields) > 0 && string(parsed.Error.Fields) != "null" {
			e.Message += " " + string(parsed.Error.Fields)
		}
	} else if len(body) > 0 {
		e.Message = strings.TrimSpace(string(body))
		if len(e.Message) > 500 {
			e.Message = e.Message[:500] + "…"
		}
	}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			e.RetryAfter = time.Duration(secs) * time.Second
		}
	}
	return e
}

func (c *Client) resolveURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.BaseURL + path
}

// Response is a successful raw response.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Do executes a request. body is buffered so idempotent requests can be
// retried on 429/502/503/504. Non-2xx responses return *Error together
// with the response (so passthrough callers can still print the body).
func (c *Client) Do(method, path string, body []byte, contentType, accept string) (*Response, error) {
	retryable := method == http.MethodGet || method == http.MethodHead
	for attempt := 0; ; attempt++ {
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, c.resolveURL(path), rdr)
		if err != nil {
			return nil, err
		}
		switch c.AuthMode() {
		case "basic":
			req.SetBasicAuth(c.Email, c.Token)
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		req.Header.Set("User-Agent", c.UserAgent)
		if accept == "" {
			accept = "application/json"
		}
		req.Header.Set("Accept", accept)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request a Bitbucket falló: %w", err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("leyendo respuesta: %w", err)
		}
		out := &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: data}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return out, nil
		}
		apiErr := decodeError(resp, data)
		if retryable && attempt < c.MaxRetries && isRetryableStatus(resp.StatusCode) {
			wait := apiErr.RetryAfter
			if wait <= 0 {
				wait = time.Duration(1<<attempt) * time.Second
			}
			if wait > 30*time.Second {
				wait = 30 * time.Second
			}
			c.Sleep(wait)
			continue
		}
		return out, apiErr
	}
}

func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

// getJSON GETs path and decodes the JSON body into out.
func (c *Client) getJSON(path string, out any) error {
	resp, err := c.Do(http.MethodGet, path, nil, "", "")
	if err != nil {
		return err
	}
	return decodeInto(resp.Body, out)
}

// sendJSON sends in (if non-nil) as JSON and decodes the response into out
// (if non-nil).
func (c *Client) sendJSON(method, path string, in, out any) error {
	var body []byte
	ct := ""
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
		ct = "application/json"
	}
	resp, err := c.Do(method, path, body, ct, "")
	if err != nil {
		return err
	}
	if out == nil || len(resp.Body) == 0 {
		return nil
	}
	return decodeInto(resp.Body, out)
}

func decodeInto(body []byte, out any) error {
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decodificando respuesta: %w", err)
	}
	return nil
}

// getText GETs a non-JSON resource (diffs, raw files). Redirects (the PR
// diff endpoint answers 302) are followed by net/http.
func (c *Client) getText(path string) ([]byte, error) {
	resp, err := c.Do(http.MethodGet, path, nil, "", "*/*")
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

type page[T any] struct {
	Values []T    `json:"values"`
	Next   string `json:"next"`
	Size   *int   `json:"size"`
}

// paginate follows "next" links until limit items (limit <= 0: all).
// pagelen is the per-endpoint page size (Bitbucket rejects values above
// each endpoint's maximum, e.g. 50 for pull request lists).
func paginate[T any](c *Client, path string, pagelen, limit int) ([]T, error) {
	if limit > 0 && limit < pagelen {
		pagelen = limit
	}
	next := withQuery(path, url.Values{"pagelen": {strconv.Itoa(pagelen)}})
	all := []T{}
	for next != "" {
		var p page[T]
		if err := c.getJSON(next, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Values...)
		if limit > 0 && len(all) >= limit {
			return all[:limit], nil
		}
		next = p.Next
	}
	return all, nil
}

// withQuery appends q to path, preserving any query already present.
func withQuery(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + q.Encode()
}
