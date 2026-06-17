// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestProviderMetadata(t *testing.T) {
	p := New("test")()
	var resp provider.MetadataResponse
	p.Metadata(context.Background(), provider.MetadataRequest{}, &resp)

	if resp.TypeName != "xema" {
		t.Fatalf("expected provider type name %q, got %q", "xema", resp.TypeName)
	}
	if resp.Version != "test" {
		t.Fatalf("expected version %q, got %q", "test", resp.Version)
	}
}

func TestProviderSchemaAttributes(t *testing.T) {
	p := New("test")()
	var resp provider.SchemaResponse
	p.Schema(context.Background(), provider.SchemaRequest{}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", resp.Diagnostics)
	}
	for _, attr := range []string{"endpoint", "org", "token"} {
		if _, ok := resp.Schema.Attributes[attr]; !ok {
			t.Errorf("provider schema missing attribute %q", attr)
		}
	}
}

func TestProviderRegistersResourcesAndDataSources(t *testing.T) {
	xp, ok := New("test")().(*xemaProvider)
	if !ok {
		t.Fatal("New did not return *xemaProvider")
	}

	resources := xp.Resources(context.Background())
	if got := len(resources); got != 15 {
		t.Fatalf("expected 15 resources, got %d", got)
	}

	wantResources := map[string]bool{
		"xema_project":               false,
		"xema_provider":              false,
		"xema_model_resolution_rule": false,
		"xema_role":                  false,
		"xema_org":                   false,
		"xema_deliverable_spec":      false,
		"xema_biome_install":         false,
		"xema_portal":                false,
		"xema_agent":                 false,
		"xema_grant":                 false,
		"xema_team":                  false,
		"xema_environment":           false,
		"xema_skill":                 false,
		"xema_model":                 false,
		"xema_space":                 false,
	}
	for _, factory := range resources {
		var resp resource.MetadataResponse
		factory().Metadata(
			context.Background(),
			resource.MetadataRequest{ProviderTypeName: "xema"},
			&resp,
		)
		if _, ok := wantResources[resp.TypeName]; ok {
			wantResources[resp.TypeName] = true
		}
	}
	for name, seen := range wantResources {
		if !seen {
			t.Errorf("resource %q not registered", name)
		}
	}
}

// TestResourceSchemasValid instantiates every registered resource and exercises
// its Schema, asserting the framework reports no diagnostics — this catches a
// malformed attribute (bad nesting, missing element type, etc.) at unit time.
func TestResourceSchemasValid(t *testing.T) {
	xp, ok := New("test")().(*xemaProvider)
	if !ok {
		t.Fatal("New did not return *xemaProvider")
	}
	for _, factory := range xp.Resources(context.Background()) {
		var meta resource.MetadataResponse
		factory().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "xema"}, &meta)
		var resp resource.SchemaResponse
		factory().Schema(context.Background(), resource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("resource %q schema diagnostics: %v", meta.TypeName, resp.Diagnostics)
		}
	}

	if got := len(xp.DataSources(context.Background())); got != 15 {
		t.Fatalf("expected 15 data sources, got %d", got)
	}
}

// TestDataSourceSchemasValid instantiates every registered data source and
// exercises its Schema, asserting the framework reports no diagnostics.
func TestDataSourceSchemasValid(t *testing.T) {
	xp, ok := New("test")().(*xemaProvider)
	if !ok {
		t.Fatal("New did not return *xemaProvider")
	}
	for _, factory := range xp.DataSources(context.Background()) {
		var meta datasource.MetadataResponse
		factory().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "xema"}, &meta)
		var resp datasource.SchemaResponse
		factory().Schema(context.Background(), datasource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("data source %q schema diagnostics: %v", meta.TypeName, resp.Diagnostics)
		}
	}
}
