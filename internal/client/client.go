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
// Every org-scoped request carries an org-admin bearer token and the canonical
// X-Xema-Org-Id tenant header. The control plane derives the authoritative org
// from the verified token; the header is sent for tenant routing parity. For
// kind `org` the org-scoped surface only reads/updates the caller's own org;
// create/delete are refused 403 ORG_LIFECYCLE_IS_INSTALLATION_SCOPED.
//
// Org lifecycle lives on the installation surface, which needs a
// platform_admin token. The X-Xema-Org-Id header is still sent (the platform
// middleware requires it of every user token) but the installation routes
// ignore it and authorize on platform_admin alone; installation authority never
// implies org admin, and vice versa:
//
//	GET    /control-plane/installation/orgs        -> list
//	POST   /control-plane/installation/orgs        -> create  (returns a handle)
//	GET    /control-plane/installation/orgs/:id     -> read
//	PUT    /control-plane/installation/orgs/:id     -> update
//	DELETE /control-plane/installation/orgs/:id     -> delete
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// orgHeader is the canonical Xema tenant-routing header.
const orgHeader = "X-Xema-Org-Id"

// Client talks to a single control-plane-api endpoint, for a single org or, with
// no org, as an operator limited to installation routes.
type Client struct {
	endpoint string
	org      string
	token    string
	http     *http.Client
	// orgPending marks a client built while the provider's org is still an
	// unknown value (plan time, org fed from another resource). It can be handed
	// to resources so they plan, but it never issues a request.
	orgPending bool
}

// New constructs a Client. endpoint is the control-plane base URL (any trailing
// slash is trimmed); org and token are the org id and bearer.
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

// NewOrgPending constructs a Client for a plan in which the provider's org is
// not yet known. Every request on it fails: the org is never guessed.
func NewOrgPending(endpoint, token string, httpClient *http.Client) *Client {
	c := New(endpoint, "", token, httpClient)
	c.orgPending = true
	return c
}

// HasOrg reports whether the client is bound to an org. A client without one
// runs in operator mode and may only call installation-scoped routes.
func (c *Client) HasOrg() bool {
	return c.org != "" || c.orgPending
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
	return c.doURL(ctx, method, c.url(kind, id), body, out)
}

func (c *Client) installationOrgURL(id string) string {
	if id == "" {
		return c.endpoint + "/control-plane/installation/orgs"
	}
	return fmt.Sprintf("%s/control-plane/installation/orgs/%s", c.endpoint, id)
}

func (c *Client) doURL(ctx context.Context, method, url string, body any, out any) error {
	if c.orgPending {
		return fmt.Errorf("the provider org is not known yet; it cannot be used before apply")
	}
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.org != "" {
		req.Header.Set(orgHeader, c.org)
	}
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

// CreateInstallationOrg creates an org on the installation surface
// (POST /control-plane/installation/orgs). It needs a platform_admin token; the
// org header is sent but ignored by the route.
func (c *Client) CreateInstallationOrg(ctx context.Context, spec map[string]any) (*Handle, error) {
	var h Handle
	if err := c.doURL(ctx, http.MethodPost, c.installationOrgURL(""), specBody{Spec: spec}, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// ReadInstallationOrg fetches an org by id on the installation surface. A 404
// surfaces as an APIError with Status 404.
func (c *Client) ReadInstallationOrg(ctx context.Context, id string) (*Resource, error) {
	var r Resource
	if err := c.doURL(ctx, http.MethodGet, c.installationOrgURL(id), nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateInstallationOrg replaces an org's spec on the installation surface.
func (c *Client) UpdateInstallationOrg(ctx context.Context, id string, spec map[string]any) (*Resource, error) {
	var r Resource
	if err := c.doURL(ctx, http.MethodPut, c.installationOrgURL(id), specBody{Spec: spec}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteInstallationOrg deletes an org on the installation surface.
func (c *Client) DeleteInstallationOrg(ctx context.Context, id string) error {
	return c.doURL(ctx, http.MethodDelete, c.installationOrgURL(id), nil, nil)
}

// IsNotFound reports whether err is an APIError with a 404 status.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if e, ok := err.(*APIError); ok {
		apiErr = e
	}
	return apiErr != nil && apiErr.Status == http.StatusNotFound
}

// IsConflict reports whether err is an APIError with a 409 status, which the
// owning service uses for an optimistic-concurrency refusal.
func IsConflict(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict
}
