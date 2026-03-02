package stdlib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// MaxHTTPResponseSize is the maximum HTTP response body size (2 MB).
const MaxHTTPResponseSize = 2 * 1024 * 1024

// DefaultHTTPTimeout is the default timeout for HTTP requests (1800s).
const DefaultHTTPTimeout = 1800 * time.Second

// RegisterHTTP registers http.* functions. This is separate because it may need
// a custom HTTP client for testing.
func (r *Registry) RegisterHTTP(client *http.Client) {
	if client == nil {
		client = &http.Client{Timeout: DefaultHTTPTimeout}
	}

	doRequest := func(method string) StdlibFunc {
		return func(args []types.Value) (types.Value, error) {
			return httpDoRequest(client, method, args)
		}
	}

	r.Register("http.get", doRequest("GET"))
	r.Register("http.post", doRequest("POST"))
	r.Register("http.put", doRequest("PUT"))
	r.Register("http.patch", doRequest("PATCH"))
	r.Register("http.delete", doRequest("DELETE"))
	r.Register("http.request", func(args []types.Value) (types.Value, error) {
		// http.request uses the method from args
		method := "GET"
		if len(args) > 0 && args[0].Type() == types.TypeMap {
			if m, ok := args[0].AsMap().Get("method"); ok {
				method = strings.ToUpper(m.AsString())
			}
		}
		return httpDoRequest(client, method, args)
	})
}

func httpDoRequest(client *http.Client, method string, args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("http.%s requires arguments", strings.ToLower(method))
	}

	var (
		requestURL string
		body       io.Reader
		headers    map[string]string
		query      map[string]string
		timeout    time.Duration
	)

	argMap := args[0]
	if argMap.Type() != types.TypeMap {
		return types.Null, types.NewTypeError("http request args must be a map")
	}
	m := argMap.AsMap()

	// URL (required)
	if u, ok := m.Get("url"); ok {
		requestURL = u.AsString()
	} else {
		return types.Null, fmt.Errorf("http.%s: missing 'url' argument", strings.ToLower(method))
	}

	// Headers
	if h, ok := m.Get("headers"); ok && h.Type() == types.TypeMap {
		headers = make(map[string]string)
		for _, k := range h.AsMap().Keys() {
			v, _ := h.AsMap().Get(k)
			headers[k] = v.String()
		}
	}

	// Query parameters
	if q, ok := m.Get("query"); ok && q.Type() == types.TypeMap {
		query = make(map[string]string)
		for _, k := range q.AsMap().Keys() {
			v, _ := q.AsMap().Get(k)
			query[k] = v.String()
		}
	}

	// Body
	if b, ok := m.Get("body"); ok {
		switch b.Type() {
		case types.TypeString:
			body = strings.NewReader(b.AsString())
		case types.TypeMap, types.TypeList:
			jsonBytes, err := b.MarshalJSON()
			if err != nil {
				return types.Null, fmt.Errorf("http.%s: failed to marshal body: %v", strings.ToLower(method), err)
			}
			body = bytes.NewReader(jsonBytes)
			if headers == nil {
				headers = make(map[string]string)
			}
			if _, exists := headers["Content-Type"]; !exists {
				headers["Content-Type"] = "application/json"
			}
		default:
			body = strings.NewReader(b.String())
		}
	}

	// Timeout
	timeout = DefaultHTTPTimeout
	if t, ok := m.Get("timeout"); ok {
		if n, numOk := t.AsNumber(); numOk {
			timeout = time.Duration(n * float64(time.Second))
		}
	}

	// Build URL with query params
	if len(query) > 0 {
		u, err := url.Parse(requestURL)
		if err != nil {
			return types.Null, types.NewValueError(fmt.Sprintf("invalid URL: %v", err))
		}
		q := u.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		requestURL = u.String()
	}

	// Create request
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return types.Null, types.NewConnectionError(
			fmt.Sprintf("failed to create request: %v", err))
	}

	// Set headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return types.Null, types.NewTimeoutError("HTTP request timed out")
		}
		// Use ConnectionFailedError for connection refusal/unreachable
		return types.Null, types.NewConnectionFailedError(
			fmt.Sprintf("HTTP request failed: %v", err))
	}
	defer resp.Body.Close()

	// Read response body (with size limit)
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, MaxHTTPResponseSize+1))
	if err != nil {
		return types.Null, types.NewConnectionError(
			fmt.Sprintf("failed to read response: %v", err))
	}
	if len(respBody) > MaxHTTPResponseSize {
		return types.Null, types.NewResourceLimitError(
			fmt.Sprintf("response size exceeds %d bytes", MaxHTTPResponseSize))
	}

	// Build response map
	result := types.NewOrderedMap()
	result.Set("code", types.NewInt(int64(resp.StatusCode)))

	// Parse response body
	bodyVal := parseResponseBody(respBody, resp.Header.Get("Content-Type"))
	result.Set("body", bodyVal)

	// Response headers (lowercase keys per GCW behavior)
	headerMap := types.NewOrderedMap()
	for k := range resp.Header {
		headerMap.Set(strings.ToLower(k), types.NewString(resp.Header.Get(k)))
	}
	result.Set("headers", types.NewMap(headerMap))

	// Check for HTTP errors (4xx, 5xx)
	if resp.StatusCode >= 400 {
		we := types.NewHttpError(
			int64(resp.StatusCode),
			fmt.Sprintf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		)
		// Attach headers and body to the error for GCW compatibility
		we.Extra = map[string]types.Value{
			"headers": types.NewMap(headerMap),
			"body":    bodyVal,
		}
		return types.Null, we
	}

	return types.NewMap(result), nil
}

// parseResponseBody tries to parse the response body as JSON, falling back to string.
func parseResponseBody(body []byte, contentType string) types.Value {
	if len(body) == 0 {
		return types.Null
	}

	// Try JSON parsing if content type suggests it or body looks like JSON
	if strings.Contains(contentType, "json") || isJSONLike(body) {
		var raw interface{}
		if err := json.Unmarshal(body, &raw); err == nil {
			return types.ValueFromJSON(raw)
		}
	}

	return types.NewString(string(body))
}

func isJSONLike(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '{', '[', '"':
			return true
		default:
			return false
		}
	}
	return false
}
