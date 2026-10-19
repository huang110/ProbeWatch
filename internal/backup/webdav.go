package backup

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// WebDAVConfig holds configuration for a WebDAV remote backup destination.
type WebDAVConfig struct {
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	// AllowPrivateNetwork is an explicit opt-in for self-hosted WebDAV stores
	// on private LANs or overlay networks. It never changes the default SSRF
	// protection applied to public backup endpoints.
	AllowPrivateNetwork bool `json:"allow_private_network"`
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
		cfg:        cfg,
		httpClient: backupHTTPClient(cfg.AllowPrivateNetwork),
	}
}

// PutFile uploads data to the WebDAV endpoint.
func (c *WebDAVClient) PutFile(ctx context.Context, filename string, data []byte) error {
	return c.putReader(ctx, filename, bytes.NewReader(data), int64(len(data)))
}

// PutFilePath uploads a local file as a stream, avoiding a full in-memory copy.
func (c *WebDAVClient) PutFilePath(ctx context.Context, filename, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat webdav upload file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("webdav upload path is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open webdav upload file: %w", err)
	}
	defer file.Close()
	return c.putReader(ctx, filename, file, info.Size())
}

func (c *WebDAVClient) putReader(ctx context.Context, filename string, data io.Reader, size int64) error {
	reqURL, err := c.endpointURL(filename)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, data)
	if err != nil {
		return fmt.Errorf("create webdav put request: %w", err)
	}

	req.Header.Set("Content-Type", "application/gzip")
	req.ContentLength = size
	req.Header.Set("Content-Length", fmt.Sprintf("%d", size))
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
	reqURL, err := c.endpointURL(filename)
	if err != nil {
		return err
	}

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

func (c *WebDAVClient) endpointURL(filename string) (string, error) {
	if err := validateBackupObjectName(filename); err != nil {
		return "", err
	}
	u, err := parseBackupEndpoint(c.cfg.URL, "")
	if err != nil {
		return "", fmt.Errorf("parse webdav endpoint: %w", err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + filename
	u.RawPath = ""
	return u.String(), nil
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
