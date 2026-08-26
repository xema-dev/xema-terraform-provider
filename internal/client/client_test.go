// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateSendsAuthAndOrgHeaders(t *testing.T) {
	var gotAuth, gotOrg, gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotOrg = r.Header.Get("X-Xema-Org-Id")
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewEncoder(w).Encode(Handle{Kind: "project", PhysicalID: "p-1", ManagedKey: "demo"})
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "", "org-123", "tok-abc", srv.Client())
	h, err := c.Create(context.Background(), "project", map[string]any{"name": "demo"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if h.PhysicalID != "p-1" {
		t.Errorf("physical id = %q, want p-1", h.PhysicalID)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/control-plane/resources/project" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotOrg != "org-123" {
		t.Errorf("org header = %q", gotOrg)
	}
}

func TestNotWiredKindSurfacesAsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"code":    "RESOURCE_KIND_NOT_WIRED",
			"message": "kind 'skill' is not wired",
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "", "org-1", "tok", srv.Client())
	_, err := c.Create(context.Background(), "skill", map[string]any{})
	if err == nil {
		t.Fatal("expected error for not-wired kind")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Status != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", apiErr.Status)
	}
	if apiErr.Code != "RESOURCE_KIND_NOT_WIRED" {
		t.Errorf("code = %q", apiErr.Code)
	}
}

func TestReadNotFoundDetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "org-1", "tok", srv.Client())
	_, err := c.Read(context.Background(), "project", "missing")
	if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound, got %v", err)
	}
}

// TestResolveDistributionLockSendsEveryRequiredField pins the resolve-lock
// request body against fleet-control's `ResolveDistributionLockRequestDto`.
//
// It exists because nothing asserted this shape, and the cost was measurable:
// `capabilityDomainOwners` became a required, non-empty field on 2026-07-26 and
// this client kept sending a two-field body, so `data.xema_distribution_lock`
// returned 422 for a month with nothing to notice it. `platformServiceSources`
// became required on 2026-08-26 and would have repeated it. A field DROPPED
// from this body is a well-formed request the server rejects — the failure is
// on the wire, not at compile time, so only an assertion over the encoded body
// can catch it.
func TestResolveDistributionLockSendsEveryRequiredField(t *testing.T) {
	var got map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"distributionId": "acme", "schemaVersion": 2},
		})
	}))
	defer srv.Close()

	c := New("", srv.URL, "org-1", "tok", srv.Client())
	lock, err := c.ResolveDistributionLock(
		context.Background(),
		map[string]any{"id": "acme"},
		[]any{map[string]any{"id": "kb"}},
		[]any{map[string]any{"domain": "kb", "biomeId": "knowledge-base"}},
		[]any{map[string]any{"name": "identity-api", "tier": "kernel"}},
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if lock["distributionId"] != "acme" {
		t.Errorf("distributionId = %v, want acme", lock["distributionId"])
	}
	if gotPath != "/provisioning/profiles/resolve-lock" {
		t.Errorf("path = %q", gotPath)
	}
	// Every field the endpoint requires must be present AND non-empty. Presence
	// alone is not enough: a nil slice encodes as `null`, which the server's
	// `@IsArray()` rejects exactly as a missing key would.
	for _, field := range []string{
		"distribution",
		"availableBiomes",
		"capabilityDomainOwners",
		"platformServiceSources",
	} {
		value, ok := got[field]
		if !ok {
			t.Errorf("request body is missing required field %q", field)
			continue
		}
		if value == nil {
			t.Errorf("required field %q encoded as null", field)
		}
	}
	if owners, ok := got["capabilityDomainOwners"].([]any); !ok || len(owners) == 0 {
		t.Error("capabilityDomainOwners must reach the wire as a non-empty array")
	}
}

// TestResolveDistributionLockRequiresFleetEndpoint keeps the failure legible:
// the operator plane is a separate endpoint from the control plane, and an
// unset one must name itself rather than POST to a relative URL.
func TestResolveDistributionLockRequiresFleetEndpoint(t *testing.T) {
	c := New("https://control.example", "", "org-1", "tok", nil)
	_, err := c.ResolveDistributionLock(context.Background(), map[string]any{}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected an error when fleet_endpoint is unset")
	}
}
