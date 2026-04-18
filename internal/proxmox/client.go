package proxmox

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is a Proxmox API client that forwards requests.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

// NewClient creates a new Proxmox API client.
func NewClient(baseURL string, insecure bool) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxmox URL: %w", err)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecure,
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	return &Client{
		baseURL: u,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Minute,
		},
	}, nil
}

// Forward sends a request to the Proxmox API and returns the response.
func (c *Client) Forward(method, path string, body io.Reader, contentType, authHeader string) (*http.Response, error) {
	parsedPath, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse path: %w", err)
	}

	targetURL := c.baseURL.JoinPath(parsedPath.Path)
	targetURL.RawQuery = parsedPath.RawQuery

	req, err := http.NewRequest(method, targetURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return c.httpClient.Do(req)
}

// ForwardRequest forwards an existing HTTP request to Proxmox.
func (c *Client) ForwardRequest(r *http.Request, body io.Reader) (*http.Response, error) {
	targetURL := c.baseURL.JoinPath(r.URL.Path)
	targetURL.RawQuery = r.URL.RawQuery

	req, err := http.NewRequest(r.Method, targetURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Copy relevant headers from original request (including auth)
	for _, header := range []string{"Content-Type", "Accept", "Authorization"} {
		if v := r.Header.Get(header); v != "" {
			req.Header.Set(header, v)
		}
	}

	return c.httpClient.Do(req)
}

// BaseURL returns the base URL of the Proxmox API.
func (c *Client) BaseURL() string {
	return c.baseURL.String()
}
