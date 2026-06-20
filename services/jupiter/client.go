package jupiter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const baseURL = "https://api.jup.ag/prediction/v1"

var httpClient = &http.Client{Timeout: 15 * time.Second}

func apiKey() string { return os.Getenv("JUPITER_API_KEY") }

func do(ctx context.Context, method, path string, params url.Values, body []byte) ([]byte, int, error) {
	u := baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey())
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("jupiter request: %w", err)
	}
	defer resp.Body.Close()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return out, resp.StatusCode, nil
}

// Get proxies a GET request to the Jupiter Predict API.
func Get(ctx context.Context, path string, params url.Values) ([]byte, int, error) {
	return do(ctx, http.MethodGet, path, params, nil)
}

// Post sends a JSON body to Jupiter and returns the raw response.
func Post(ctx context.Context, path string, body []byte) ([]byte, int, error) {
	return do(ctx, http.MethodPost, path, nil, body)
}

// Delete sends a DELETE with optional JSON body to Jupiter.
func Delete(ctx context.Context, path string, body []byte) ([]byte, int, error) {
	return do(ctx, http.MethodDelete, path, nil, body)
}
