// Package petstore provides an HTTP client for the Swagger Petstore API
// (https://petstore.swagger.io/v2).
package petstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const baseURL = "https://petstore.swagger.io/v2"

// Client is a thin HTTP wrapper around the Petstore REST API.
type Client struct {
	base string
	http *http.Client
}

// NewClient creates a Client pointed at the public Petstore endpoint.
func NewClient() *Client {
	return &Client{
		base: baseURL,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// ---------------------------------------------------------------------------
// Low-level helpers
// ---------------------------------------------------------------------------

func (c *Client) Get(path string, query url.Values) ([]byte, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("api_key", "special-key")
	return c.do(req)
}

func (c *Client) Post(path string, body interface{}) ([]byte, error) {
	return c.sendJSON(http.MethodPost, path, body)
}

func (c *Client) Put(path string, body interface{}) ([]byte, error) {
	return c.sendJSON(http.MethodPut, path, body)
}

func (c *Client) Delete(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodDelete, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("api_key", "special-key")
	return c.do(req)
}

func (c *Client) PostForm(path string, form url.Values) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, c.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("api_key", "special-key")
	return c.do(req)
}

func (c *Client) sendJSON(method, path string, body interface{}) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_key", "special-key")
	return c.do(req)
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d %s: %s", resp.StatusCode, req.URL.Path, strings.TrimSpace(string(body)))
	}
	return body, nil
}
