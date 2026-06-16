// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

// Package client is a thin HTTP client for the Xema control-plane-api
// per-resource REST surface that the Terraform provider binds to.
//
// The surface, per kind, is:
//
//	GET    /control-plane/resources/:kind          -> list
//	POST   /control-plane/resources/:kind          -> create  (returns a handle)
//	GET    /control-plane/resources/:kind/:id       -> read    (returns the live spec)
//	PUT    /control-plane/resources/:kind/:id       -> update
//	DELETE /control-plane/resources/:kind/:id       -> delete
//
// Every request carries an org-admin bearer token and the canonical
// X-Xema-Org-Id tenant header. The control plane derives the authoritative org
// from the verified token; the header is sent for tenant routing parity.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// orgHeader is the canonical Xema tenant-routing header.
const orgHeader = "X-Xema-Org-Id"

// Client talks to a single control-plane-api endpoint for a single org.
type Client struct {
	endpoint string
	org      string
	token    string
	http     *http.Client
}

// New constructs a Client. endpoint is the control-plane base URL (any trailing
// slash is trimmed); org and token are the org id and org-admin bearer.
func New(endpoint, org, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		org:      org,
		token:    token,
		http:     httpClient,
	}
}

// Handle is the create-time response: the service-minted physical id plus the
// stable managed key proving IaC ownership.
type Handle struct {
	Kind       string `json:"kind"`
	PhysicalID string `json:"physicalId"`
	ManagedKey string `json:"managedKey"`
}

// Resource is the read-back view of a managed resource.
type Resource struct {
	Kind       string         `json:"kind"`
	PhysicalID string         `json:"physicalId"`
	ManagedKey string         `json:"managedKey"`
	Spec       map[string]any `json:"spec"`
}

// APIError is a non-2xx response from the control plane. The Code/Message are
// surfaced from the JSON error envelope when present so a 501 NOT_WIRED or a
// 4xx from the owning service reads clearly.
type APIError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
	body    string
}

func (e *APIError) Error() string {
	if e.Code != "" || e.Message != "" {
		return fmt.Sprintf("control-plane responded %d: %s %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("control-plane responded %d: %s", e.Status, e.body)
}

func (c *Client) url(kind, id string) string {
	if id == "" {
		return fmt.Sprintf("%s/control-plane/resources/%s", c.endpoint, kind)
	}
	return fmt.Sprintf("%s/control-plane/resources/%s/%s", c.endpoint, kind, id)
}

func (c *Client) do(ctx context.Context, method, kind, id string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url(kind, id), rdr)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set(orgHeader, c.org)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call control-plane: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode, body: string(raw)}
		_ = json.Unmarshal(raw, apiErr) // best-effort envelope decode
		return apiErr
	}

	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// SpecBody is the create/update request body: the kind-native spec forwarded
// verbatim to the owning service.
type specBody struct {
	Spec map[string]any `json:"spec"`
}

// Create posts a new resource of kind and returns its handle.
func (c *Client) Create(ctx context.Context, kind string, spec map[string]any) (*Handle, error) {
	var h Handle
	if err := c.do(ctx, http.MethodPost, kind, "", specBody{Spec: spec}, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// Read fetches a single resource by physical id. A 404 surfaces as an APIError
// with Status 404 so callers can treat it as "gone".
func (c *Client) Read(ctx context.Context, kind, id string) (*Resource, error) {
	var r Resource
	if err := c.do(ctx, http.MethodGet, kind, id, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Update replaces the spec of a resource by physical id and returns the
// read-back resource.
func (c *Client) Update(ctx context.Context, kind, id string, spec map[string]any) (*Resource, error) {
	var r Resource
	if err := c.do(ctx, http.MethodPut, kind, id, specBody{Spec: spec}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Delete removes a resource by physical id.
func (c *Client) Delete(ctx context.Context, kind, id string) error {
	return c.do(ctx, http.MethodDelete, kind, id, nil, nil)
}

// IsNotFound reports whether err is an APIError with a 404 status.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if e, ok := err.(*APIError); ok {
		apiErr = e
	}
	return apiErr != nil && apiErr.Status == http.StatusNotFound
}
