// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The spec a strategy declares and the spec the control plane reads back must be
// the SAME shape, or every `plan` over an unchanged declaration reports drift.
func TestModelStrategyRoundTripsItsSpec(t *testing.T) {
	declared := modelStrategyModel{
		Slug:        types.StringValue("house"),
		DisplayName: types.StringValue("House strategy"),
		Description: types.StringValue("Editorial workloads"),
	}

	spec := declared.toSpec()
	if spec["slug"] != "house" || spec["displayName"] != "House strategy" || spec["description"] != "Editorial workloads" {
		t.Fatalf("strategy fields did not reach the spec: %#v", spec)
	}
	if len(spec) != 3 {
		t.Fatalf("a strategy is a named container; the spec carries only slug, displayName, description: %#v", spec)
	}

	var readback modelStrategyModel
	readback.applyReadback(spec)
	if readback.Slug.ValueString() != "house" || readback.DisplayName.ValueString() != "House strategy" {
		t.Fatalf("fields did not round-trip: %#v", readback)
	}
	if readback.Description.ValueString() != "Editorial workloads" {
		t.Fatalf("description did not round-trip: %v", readback.Description)
	}
}

// A strategy that states no description reads back NULL, not an empty string.
func TestModelStrategyAbsentDescriptionReadsBackNull(t *testing.T) {
	var readback modelStrategyModel
	readback.applyReadback(map[string]any{"slug": "s", "displayName": "S"})
	if !readback.Description.IsNull() {
		t.Fatalf("absent description became %v, not null", readback.Description)
	}
}

// The schema is a named container: no entries block, no catalogue fields, no
// is_default. `description` is server-owned (Optional + Computed) so a create
// that omits it does not read back a value the plan did not contain.
func TestModelStrategySchemaIsANamedContainer(t *testing.T) {
	var resp resource.SchemaResponse
	(&modelStrategyResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)

	for _, gone := range []string{
		"entries", "is_default", "tier", "provider_focus",
		"default_model_slug", "default_temperature", "default_reasoning_effort",
	} {
		if _, ok := resp.Schema.Attributes[gone]; ok {
			t.Errorf("attribute %q was removed upstream and must not be in the schema", gone)
		}
	}

	desc, ok := resp.Schema.Attributes["description"]
	if !ok || !desc.IsComputed() || !desc.IsOptional() {
		t.Errorf("description must be Optional and Computed")
	}
	if id := resp.Schema.Attributes["id"]; !id.IsComputed() || id.IsOptional() {
		t.Errorf("id must be Computed and not Optional")
	}
	if slug := resp.Schema.Attributes["slug"]; slug.IsComputed() || !slug.IsRequired() {
		t.Errorf("slug is declared by the practitioner: Required, never Computed")
	}
}
