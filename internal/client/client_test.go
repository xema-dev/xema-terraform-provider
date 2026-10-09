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

	c := New(srv.URL+"/", "org-123", "tok-abc", srv.Client())
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

	c := New(srv.URL, "org-1", "tok", srv.Client())
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

	c := New(srv.URL, "org-1", "tok", srv.Client())
	_, err := c.Read(context.Background(), "project", "missing")
	if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound, got %v", err)
	}
}

func TestInstallationOrgMethodsUseInstallationPaths(t *testing.T) {
	type call struct{ method, path, org, auth string }
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org := r.Header.Get("X-Xema-Org-Id")
		calls = append(calls, call{r.Method, r.URL.Path, org, r.Header.Get("Authorization")})
		switch r.Method {
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(Handle{Kind: "org", PhysicalID: "o-1", ManagedKey: "acme"})
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			_ = json.NewEncoder(w).Encode(Resource{Kind: "org", PhysicalID: "o-1"})
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "org-123", "tok-pa", srv.Client())
	ctx := context.Background()
	if _, err := c.CreateInstallationOrg(ctx, map[string]any{"name": "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadInstallationOrg(ctx, "o-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateInstallationOrg(ctx, "o-1", map[string]any{"name": "acme"}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteInstallationOrg(ctx, "o-1"); err != nil {
		t.Fatal(err)
	}

	want := []call{
		{http.MethodPost, "/control-plane/installation/orgs", "org-123", "Bearer tok-pa"},
		{http.MethodGet, "/control-plane/installation/orgs/o-1", "org-123", "Bearer tok-pa"},
		{http.MethodPut, "/control-plane/installation/orgs/o-1", "org-123", "Bearer tok-pa"},
		{http.MethodDelete, "/control-plane/installation/orgs/o-1", "org-123", "Bearer tok-pa"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("call %d = %+v, want %+v", i, calls[i], want[i])
		}
	}
}

func TestOrgScopedCallsStillSendOrgHeader(t *testing.T) {
	var gotOrg, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrg = r.Header.Get("X-Xema-Org-Id")
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(Resource{Kind: "org", PhysicalID: "o-1"})
	}))
	defer srv.Close()

	c := New(srv.URL, "org-123", "tok", srv.Client())
	if _, err := c.Read(context.Background(), "org", "o-1"); err != nil {
		t.Fatal(err)
	}
	if gotOrg != "org-123" || gotPath != "/control-plane/resources/org/o-1" {
		t.Errorf("org=%q path=%q", gotOrg, gotPath)
	}
}
