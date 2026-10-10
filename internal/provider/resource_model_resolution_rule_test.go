// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestModelResolutionRuleRoundTripsItsSpec(t *testing.T) {
	declared := mrrModel{
		StrategyID:            types.StringValue("strat-1"),
		Selector:              &selectorModel{ModelLane: types.StringValue("coder"), Extra: types.MapNull(types.StringType)},
		TargetModelID:         types.StringValue("model-uuid"),
		TargetTemperature:     types.Float64Value(0.2),
		TargetReasoningEffort: types.StringValue("high"),
		Priority:              types.Int64Value(10),
	}

	spec, err := declared.toSpec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if spec["targetModelId"] != "model-uuid" || spec["strategyId"] != "strat-1" || spec["targetReasoningEffort"] != "high" {
		t.Fatalf("rule fields did not reach the spec: %#v", spec)
	}
	for _, gone := range []string{"targetKind", "targetProviderSlug", "targetModelLane", "isDefault"} {
		if _, ok := spec[gone]; ok {
			t.Errorf("%q was removed upstream and must not be written", gone)
		}
	}

	var readback mrrModel
	if err := readback.applyReadback(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if readback.TargetModelID.ValueString() != "model-uuid" || readback.StrategyID.ValueString() != "strat-1" {
		t.Fatalf("ids did not round-trip: %#v", readback)
	}
	if readback.Selector == nil || readback.Selector.ModelLane.ValueString() != "coder" {
		t.Fatalf("selector did not round-trip: %#v", readback.Selector)
	}
	if readback.Priority.ValueInt64() != 10 || readback.TargetTemperature.ValueFloat64() != 0.2 {
		t.Fatalf("numbers did not round-trip: %#v", readback)
	}
}

// An org-level rule has no strategy: strategy_id reads back NULL, not "".
func TestModelResolutionRuleOrgLevelHasNoStrategy(t *testing.T) {
	declared := mrrModel{
		Selector:      &selectorModel{Project: types.StringValue("p1"), Extra: types.MapNull(types.StringType)},
		TargetModelID: types.StringValue("m"),
	}
	spec, err := declared.toSpec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec["strategyId"]; ok {
		t.Fatalf("an org-level rule must not write strategyId: %#v", spec)
	}
	var readback mrrModel
	if err := readback.applyReadback(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if !readback.StrategyID.IsNull() {
		t.Fatalf("absent strategy became %v, not null", readback.StrategyID)
	}
}

func TestModelResolutionRuleSchema(t *testing.T) {
	var resp resource.SchemaResponse
	(&modelResolutionRuleResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)

	for _, gone := range []string{"target_kind", "target_provider_slug", "target_model_lane", "is_default"} {
		if _, ok := resp.Schema.Attributes[gone]; ok {
			t.Errorf("attribute %q was removed upstream and must not be in the schema", gone)
		}
	}
	if a := resp.Schema.Attributes["target_model_id"]; !a.IsRequired() {
		t.Errorf("target_model_id is required")
	}
	if a := resp.Schema.Attributes["selector"]; !a.IsRequired() {
		t.Errorf("selector is required: an empty selector is refused upstream")
	}
	if a, ok := resp.Schema.Attributes["strategy_id"]; !ok || !a.IsOptional() {
		t.Errorf("strategy_id must be Optional")
	}
}
