// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

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
	if got := len(resources); got != 3 {
		t.Fatalf("expected 3 resources, got %d", got)
	}

	wantResources := map[string]bool{
		"xema_project":               false,
		"xema_provider":              false,
		"xema_model_resolution_rule": false,
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

	if got := len(xp.DataSources(context.Background())); got != 1 {
		t.Fatalf("expected 1 data source, got %d", got)
	}
}
