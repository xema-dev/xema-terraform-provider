// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The spec a strategy declares and the spec the control plane reads back must be
// the SAME shape, or every `plan` over an unchanged declaration reports drift.
// This is the one part of the resource that is pure and the one most likely to
// be wrong, so it is asserted directly rather than through a live apply.
func TestModelStrategyRoundTripsItsSpec(t *testing.T) {
	declared := modelStrategyModel{
		Slug:               types.StringValue("presenca-default"),
		DisplayName:        types.StringValue("Presenca default"),
		Description:        types.StringValue("Editorial workloads"),
		Tier:               types.StringValue("balanced"),
		ProviderFocus:      types.StringValue("mixed"),
		IsDefault:          types.BoolValue(true),
		DefaultModelSlug:   types.StringValue("model-a"),
		DefaultTemperature: types.Float64Value(0.2),
		Entries: []strategyEntryModel{
			// Deliberately NOT in sorted order: the readback sorts, and a test
			// that fed it sorted input could not tell a working sort from none.
			{ModelLane: types.StringValue("planner"), ModelSlug: types.StringValue("model-b")},
			{ModelLane: types.StringValue("coder"), ModelSlug: types.StringValue("model-c"),
				ReasoningEffort: types.StringValue("high")},
		},
	}

	spec := declared.toSpec()

	if spec["slug"] != "presenca-default" || spec["defaultModelSlug"] != "model-a" {
		t.Fatalf("strategy fields did not reach the spec: %#v", spec)
	}
	if spec["isDefault"] != true {
		t.Fatalf("isDefault did not reach the spec: %#v", spec["isDefault"])
	}
	entries, ok := spec["entries"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("expected 2 entries in the spec, got %#v", spec["entries"])
	}

	var readback modelStrategyModel
	readback.applyReadback(spec)

	if readback.Slug.ValueString() != "presenca-default" {
		t.Fatalf("slug did not round-trip: %q", readback.Slug.ValueString())
	}
	if readback.DefaultTemperature.ValueFloat64() != 0.2 {
		t.Fatalf("defaultTemperature did not round-trip: %v", readback.DefaultTemperature)
	}
	if !readback.IsDefault.ValueBool() {
		t.Fatalf("isDefault did not round-trip")
	}
	if len(readback.Entries) != 2 {
		t.Fatalf("expected 2 entries after readback, got %d", len(readback.Entries))
	}
	if readback.Entries[0].ModelLane.ValueString() != "coder" {
		t.Fatalf("entries were not sorted by lane: %q first", readback.Entries[0].ModelLane.ValueString())
	}
	if readback.Entries[0].ReasoningEffort.ValueString() != "high" {
		t.Fatalf("reasoningEffort did not round-trip: %v", readback.Entries[0].ReasoningEffort)
	}
	// An entry that set no temperature must come back NULL, not 0. Zero is a
	// legal temperature, so a lossy mapping here would silently pin every
	// unspecified lane to fully deterministic sampling.
	if !readback.Entries[0].Temperature.IsNull() {
		t.Fatalf("absent temperature became %v, not null", readback.Entries[0].Temperature)
	}
}

// An absent `entries` block and an empty one are different declarations: the
// upstream write is a whole-set REPLACE, so emitting `[]` for an absent block
// would strip every lane binding from a declaration that simply did not mention
// them.
func TestModelStrategyDistinguishesAbsentEntriesFromEmpty(t *testing.T) {
	absent := modelStrategyModel{
		Slug: types.StringValue("s"), DisplayName: types.StringValue("S"),
		Tier: types.StringValue("balanced"), ProviderFocus: types.StringValue("mixed"),
		DefaultModelSlug: types.StringValue("m"),
	}
	if _, present := absent.toSpec()["entries"]; present {
		t.Fatalf("an absent entries block must not reach the spec at all")
	}

	empty := absent
	empty.Entries = []strategyEntryModel{}
	entries, present := empty.toSpec()["entries"]
	if !present {
		t.Fatalf("an explicitly empty entries block must reach the spec")
	}
	if got := entries.([]any); len(got) != 0 {
		t.Fatalf("expected an empty entry list, got %#v", got)
	}
}
