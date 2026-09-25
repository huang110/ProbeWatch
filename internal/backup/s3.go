package backup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// S3Config holds configuration for an S3-compatible remote backup target.
type S3Config struct {
	Enabled        bool   `json:"enabled"`
	Endpoint       string `json:"endpoint"`
	Region         string `json:"region"`
	Bucket         string `json:"bucket"`
	Prefix         string `json:"prefix"`
	AccessKey      string `json:"access_key"`
	SecretKey      string `json:"secret_key"`
	ForcePathStyle bool   `json:"force_path_style"`
}

// Masked returns a safe copy of S3Config with secret key masked for API responses.
func (c S3Config) Masked() S3Config {
	masked := c
	if len(masked.SecretKey) > 4 {
		masked.SecretKey = masked.SecretKey[:2] + "••••••••" + masked.SecretKey[len(masked.SecretKey)-2:]
	} else if len(masked.SecretKey) > 0 {
		masked.SecretKey = "••••••••"
	}
	return masked
}

// S3Client provides pure-Go S3 API operations using AWS Signature Version 4.
type S3Client struct {
	cfg        S3Config
	httpClient *http.Client
}

// NewS3Client creates a new S3Client.
func NewS3Client(cfg S3Config) *S3Client {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return &S3Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// PutObject uploads data to S3 with content hashing and SigV4 authentication.
func (c *S3Client) PutObject(ctx context.Context, objectKey string, data []byte, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	fullKey := c.buildKey(objectKey)
	reqURL, err := c.buildURL(fullKey)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create put request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(data)))

	payloadHash := sha256Hex(data)
	if err := c.signRequest(req, payloadHash); err != nil {
		return fmt.Errorf("sign s3 request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute s3 put: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("s3 put failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// DeleteObject deletes an object from the S3 bucket.
func (c *S3Client) DeleteObject(ctx context.Context, objectKey string) error {
	fullKey := c.buildKey(objectKey)
	reqURL, err := c.buildURL(fullKey)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}

	payloadHash := sha256Hex(nil)
	if err := c.signRequest(req, payloadHash); err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("s3 delete failed with status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// TestConnection tests S3 connectivity by putting and deleting a probe object.
func (c *S3Client) TestConnection(ctx context.Context) error {
	if c.cfg.Bucket == "" {
		return errors.New("bucket name is required")
	}
	if c.cfg.AccessKey == "" || c.cfg.SecretKey == "" {
		return errors.New("access key and secret key are required")
	}

	probeKey := fmt.Sprintf(".probewatch-probe-%d.tmp", time.Now().UnixNano())
	probeContent := []byte("probewatch-s3-connectivity-check")

	if err := c.PutObject(ctx, probeKey, probeContent, "text/plain"); err != nil {
		return fmt.Errorf("test put failed: %w", err)
	}
	_ = c.DeleteObject(ctx, probeKey)
	return nil
}

func (c *S3Client) buildKey(key string) string {
	prefix := strings.Trim(c.cfg.Prefix, "/")
	key = strings.TrimLeft(key, "/")
	if prefix == "" {
		return key
	}
	return prefix + "/" + key
}

func (c *S3Client) buildURL(key string) (string, error) {
	endpoint := c.cfg.Endpoint
	if endpoint == "" {
		endpoint = "https://s3.amazonaws.com"
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "https://" + endpoint
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}

	// Always path style for compatibility with R2, MinIO, OSS
	path := strings.TrimRight(u.Path, "/")
	if c.cfg.Bucket != "" {
		path += "/" + c.cfg.Bucket
	}
	if key != "" {
		path += "/" + key
	}

	u.Path = path
	return u.String(), nil
}

// signRequest signs the HTTP request using AWS Signature Version 4.
func (c *S3Client) signRequest(req *http.Request, payloadHash string) error {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	u, err := url.Parse(req.URL.String())
	if err != nil {
		return err
	}
	req.Header.Set("Host", u.Host)

	// Canonical headers
	headersToSign := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date"}
	var headerEntries []string
	var signedHeadersList []string

	for _, h := range headersToSign {
		val := req.Header.Get(h)
		if val != "" || h == "content-type" {
			hLower := strings.ToLower(h)
			headerEntries = append(headerEntries, fmt.Sprintf("%s:%s", hLower, strings.TrimSpace(val)))
			signedHeadersList = append(signedHeadersList, hLower)
		}
	}
	sort.Strings(headerEntries)
	sort.Strings(signedHeadersList)

	canonicalHeaders := strings.Join(headerEntries, "\n") + "\n"
	signedHeaders := strings.Join(signedHeadersList, ";")

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	canonicalQuery := req.URL.Query().Encode()

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	)

	credentialScope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, c.cfg.Region)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	)

	signingKey := getSignatureKey(c.cfg.SecretKey, dateStamp, c.cfg.Region, "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.cfg.AccessKey,
		credentialScope,
		signedHeaders,
		signature,
	)

	req.Header.Set("Authorization", authHeader)
	return nil
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func getSignatureKey(secret, dateStamp, regionName, serviceName string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(regionName))
	kService := hmacSHA256(kRegion, []byte(serviceName))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}
