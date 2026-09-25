package backup

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// WebDAVConfig holds configuration for a WebDAV remote backup destination.
type WebDAVConfig struct {
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Masked returns a safe copy of WebDAVConfig with password masked for API responses.
func (c WebDAVConfig) Masked() WebDAVConfig {
	masked := c
	if len(masked.Password) > 0 {
		masked.Password = "••••••••"
	}
	return masked
}

// WebDAVClient performs remote file uploads to WebDAV servers.
type WebDAVClient struct {
	cfg        WebDAVConfig
	httpClient *http.Client
}

// NewWebDAVClient creates a new WebDAVClient.
func NewWebDAVClient(cfg WebDAVConfig) *WebDAVClient {
	return &WebDAVClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// PutFile uploads data to the WebDAV endpoint.
func (c *WebDAVClient) PutFile(ctx context.Context, filename string, data []byte) error {
	reqURL := strings.TrimRight(c.cfg.URL, "/") + "/" + strings.TrimLeft(filename, "/")

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create webdav put request: %w", err)
	}

	req.Header.Set("Content-Type", "application/gzip")
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(data)))
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute webdav put: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("webdav put failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// DeleteFile deletes a file on the WebDAV server.
func (c *WebDAVClient) DeleteFile(ctx context.Context, filename string) error {
	reqURL := strings.TrimRight(c.cfg.URL, "/") + "/" + strings.TrimLeft(filename, "/")

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("webdav delete failed with status %d", resp.StatusCode)
	}
	return nil
}

// TestConnection tests WebDAV connectivity by putting and deleting a probe file.
func (c *WebDAVClient) TestConnection(ctx context.Context) error {
	if c.cfg.URL == "" {
		return errors.New("webdav url is required")
	}

	probeName := fmt.Sprintf(".probewatch-probe-%d.tmp", time.Now().UnixNano())
	probeData := []byte("probewatch-webdav-connectivity-check")

	if err := c.PutFile(ctx, probeName, probeData); err != nil {
		return fmt.Errorf("test put failed: %w", err)
	}
	_ = c.DeleteFile(ctx, probeName)
	return nil
}

func (c *WebDAVClient) setAuth(req *http.Request) {
	if c.cfg.Username != "" || c.cfg.Password != "" {
		auth := c.cfg.Username + ":" + c.cfg.Password
		encoded := base64.StdEncoding.EncodeToString([]byte(auth))
		req.Header.Set("Authorization", "Basic "+encoded)
	}
}
