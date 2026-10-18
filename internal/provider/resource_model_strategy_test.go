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
	ctx := context.Background()
	declared := modelStrategyModel{
		Slug:        types.StringValue("house"),
		DisplayName: types.StringValue("House strategy"),
		Description: types.StringValue("Editorial workloads"),
		Rules: []ruleBodyModel{{
			Selector:              &selectorModel{ModelLane: types.StringValue("coder"), Extra: types.MapNull(types.StringType)},
			TargetModelID:         types.StringValue("model-uuid"),
			TargetReasoningEffort: types.StringValue("high"),
			TargetTemperature:     types.Float64Null(),
			Priority:              types.Int64Value(20),
		}},
	}

	spec, err := declared.toSpec(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if spec["slug"] != "house" || spec["displayName"] != "House strategy" || spec["description"] != "Editorial workloads" {
		t.Fatalf("strategy fields did not reach the spec: %#v", spec)
	}
	if len(spec) != 4 {
		t.Fatalf("a strategy is a named container; the spec carries only slug, displayName, description, rules: %#v", spec)
	}
	rules, ok := spec["rules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("rules did not reach the spec: %#v", spec["rules"])
	}
	if rule := rules[0].(map[string]any); rule["targetModelId"] != "model-uuid" || rule["priority"] != int64(20) {
		t.Fatalf("a rule targets a model id and carries its priority: %#v", rule)
	}

	var readback modelStrategyModel
	if err := readback.applyReadback(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if readback.Slug.ValueString() != "house" || readback.DisplayName.ValueString() != "House strategy" {
		t.Fatalf("fields did not round-trip: %#v", readback)
	}
	if readback.Description.ValueString() != "Editorial workloads" {
		t.Fatalf("description did not round-trip: %v", readback.Description)
	}
	if len(readback.Rules) != 1 || readback.Rules[0].TargetModelID.ValueString() != "model-uuid" ||
		readback.Rules[0].Selector == nil || readback.Rules[0].Selector.ModelLane.ValueString() != "coder" ||
		readback.Rules[0].Priority.ValueInt64() != 20 {
		t.Fatalf("rules did not round-trip: %#v", readback.Rules)
	}
}

// `rules = []` is a declaration (replace with none), so an empty set reads back as
// an empty set and is written as an empty array, never null or absent.
func TestModelStrategyEmptyRulesStayEmpty(t *testing.T) {
	ctx := context.Background()
	declared := modelStrategyModel{Slug: types.StringValue("s"), DisplayName: types.StringValue("S"), Rules: []ruleBodyModel{}}
	spec, err := declared.toSpec(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rules, ok := spec["rules"].([]any); !ok || len(rules) != 0 {
		t.Fatalf("empty rules must be written as an empty array: %#v", spec["rules"])
	}
	var readback modelStrategyModel
	if err := readback.applyReadback(ctx, map[string]any{"slug": "s", "displayName": "S"}); err != nil {
		t.Fatal(err)
	}
	if readback.Rules == nil {
		t.Fatalf("absent rules must read back as an empty set, not null")
	}
}

// A strategy that states no description reads back NULL, not an empty string.
func TestModelStrategyAbsentDescriptionReadsBackNull(t *testing.T) {
	var readback modelStrategyModel
	if err := readback.applyReadback(context.Background(), map[string]any{"slug": "s", "displayName": "S"}); err != nil {
		t.Fatal(err)
	}
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

	if rules := resp.Schema.Attributes["rules"]; rules == nil || !rules.IsRequired() {
		t.Errorf("rules must be a Required nested set: the create body requires it")
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
