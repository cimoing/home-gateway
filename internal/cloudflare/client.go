package cloudflare

import (
	"bytes"
	"context"
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

const defaultBaseURL = "https://api.cloudflare.com/client/v4/"

var ErrNotFound = errors.New("Cloudflare resource not found")

// Client is a minimal Cloudflare v4 API client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the API base URL, primarily for tests.
func WithBaseURL(baseURL string) Option {
	return func(client *Client) {
		client.baseURL = strings.TrimRight(baseURL, "/") + "/"
	}
}

// WithHTTPClient overrides the HTTP transport.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *Client) {
		client.httpClient = httpClient
	}
}

// NewClient creates an API client using a Bearer API Token.
func NewClient(token string, options ...Option) *Client {
	client := &Client{
		baseURL: defaultBaseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
	for _, option := range options {
		option(client)
	}
	return client
}

// Zone is a Cloudflare DNS zone.
type Zone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Record is a Cloudflare DNS record.
type Record struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Content    string         `json:"content"`
	TTL        int            `json:"ttl"`
	Proxied    *bool          `json:"proxied,omitempty"`
	Priority   *int           `json:"priority,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	Comment    string         `json:"comment,omitempty"`
	CreatedOn  *time.Time     `json:"created_on,omitempty"`
	ModifiedOn *time.Time     `json:"modified_on,omitempty"`
}

// RecordInput is the supported record mutation payload.
type RecordInput struct {
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Content  string         `json:"content,omitempty"`
	TTL      int            `json:"ttl"`
	Proxied  *bool          `json:"proxied,omitempty"`
	Priority *int           `json:"priority,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Comment  string         `json:"comment,omitempty"`
}

// VerifyToken checks that the configured API Token is active.
func (c *Client) VerifyToken(ctx context.Context) error {
	var result struct {
		Status string `json:"status"`
	}
	if err := c.request(ctx, http.MethodGet, "user/tokens/verify", nil, &result); err != nil {
		return err
	}
	if result.Status != "active" {
		return errors.New("Cloudflare API token is not active")
	}
	return nil
}

// FindZone resolves an exact zone name.
func (c *Client) FindZone(ctx context.Context, name string) (Zone, error) {
	path := "zones?name=" + url.QueryEscape(name) + "&page=1&per_page=50"
	var zones []Zone
	if err := c.request(ctx, http.MethodGet, path, nil, &zones); err != nil {
		return Zone{}, err
	}
	for _, zone := range zones {
		if strings.EqualFold(zone.Name, name) {
			return zone, nil
		}
	}
	return Zone{}, ErrNotFound
}

// ListRecords returns every page of DNS records for a zone.
func (c *Client) ListRecords(ctx context.Context, zoneID string) ([]Record, error) {
	const perPage = 100
	var records []Record
	for page := 1; ; page++ {
		path := fmt.Sprintf(
			"zones/%s/dns_records?page=%d&per_page=%d",
			url.PathEscape(zoneID),
			page,
			perPage,
		)
		var pageRecords []Record
		info, err := c.requestWithInfo(ctx, http.MethodGet, path, nil, &pageRecords)
		if err != nil {
			return nil, err
		}
		records = append(records, pageRecords...)
		if info.TotalPages > 0 {
			if page >= info.TotalPages {
				break
			}
		} else if len(pageRecords) < perPage {
			break
		}
	}
	return records, nil
}

// CreateRecord creates a DNS record.
func (c *Client) CreateRecord(
	ctx context.Context,
	zoneID string,
	input RecordInput,
) (Record, error) {
	var record Record
	path := fmt.Sprintf("zones/%s/dns_records", url.PathEscape(zoneID))
	if err := c.request(ctx, http.MethodPost, path, input, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// UpdateRecord overwrites a DNS record.
func (c *Client) UpdateRecord(
	ctx context.Context,
	zoneID string,
	recordID string,
	input RecordInput,
) (Record, error) {
	var record Record
	path := fmt.Sprintf(
		"zones/%s/dns_records/%s",
		url.PathEscape(zoneID),
		url.PathEscape(recordID),
	)
	if err := c.request(ctx, http.MethodPut, path, input, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// DeleteRecord deletes a DNS record.
func (c *Client) DeleteRecord(ctx context.Context, zoneID string, recordID string) error {
	path := fmt.Sprintf(
		"zones/%s/dns_records/%s",
		url.PathEscape(zoneID),
		url.PathEscape(recordID),
	)
	var result struct {
		ID string `json:"id"`
	}
	return c.request(ctx, http.MethodDelete, path, nil, &result)
}

// APIError is a safe representation of a Cloudflare API failure.
type APIError struct {
	StatusCode int
	Code       int
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("Cloudflare API error %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("Cloudflare API returned HTTP %d", e.StatusCode)
}

type responseEnvelope[T any] struct {
	Success    bool            `json:"success"`
	Result     T               `json:"result"`
	Errors     []responseError `json:"errors"`
	ResultInfo resultInfo      `json:"result_info"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type resultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalPages int `json:"total_pages"`
}

func (c *Client) request(
	ctx context.Context,
	method string,
	path string,
	body any,
	result any,
) error {
	_, err := c.requestWithInfo(ctx, method, path, body, result)
	return err
}

func (c *Client) requestWithInfo(
	ctx context.Context,
	method string,
	path string,
	body any,
	result any,
) (resultInfo, error) {
	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return resultInfo{}, fmt.Errorf("encode Cloudflare request: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return resultInfo{}, fmt.Errorf("create Cloudflare request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return resultInfo{}, fmt.Errorf("call Cloudflare API: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return resultInfo{}, fmt.Errorf("read Cloudflare response: %w", err)
	}

	var envelope responseEnvelope[json.RawMessage]
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return resultInfo{}, &APIError{StatusCode: response.StatusCode}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.Success {
		apiError := &APIError{StatusCode: response.StatusCode}
		if len(envelope.Errors) > 0 {
			apiError.Code = envelope.Errors[0].Code
			apiError.Message = envelope.Errors[0].Message
		}
		return resultInfo{}, apiError
	}
	if result != nil && len(envelope.Result) > 0 && string(envelope.Result) != "null" {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return resultInfo{}, fmt.Errorf("decode Cloudflare result: %w", err)
		}
	}
	return envelope.ResultInfo, nil
}

// IsStatus reports whether err is a Cloudflare HTTP status.
func IsStatus(err error, status int) bool {
	var apiError *APIError
	return errors.As(err, &apiError) && apiError.StatusCode == status
}

// ParseRetryAfter parses a Cloudflare Retry-After value when available.
func ParseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
