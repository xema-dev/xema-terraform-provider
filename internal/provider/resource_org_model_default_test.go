// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestOrgModelDefaultRoundTripsItsSpec(t *testing.T) {
	declared := orgModelDefaultModel{ModelID: types.StringValue("model-uuid")}
	spec := declared.toSpec()
	if spec["modelId"] != "model-uuid" || len(spec) != 1 {
		t.Fatalf("unexpected spec: %#v", spec)
	}
	var readback orgModelDefaultModel
	readback.applyReadback(spec)
	if readback.ModelID.ValueString() != "model-uuid" {
		t.Fatalf("model_id did not round-trip: %v", readback.ModelID)
	}
}

func TestOrgModelDefaultSchema(t *testing.T) {
	var resp resource.SchemaResponse
	(&orgModelDefaultResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)

	if a := resp.Schema.Attributes["model_id"]; !a.IsRequired() {
		t.Errorf("model_id is required")
	}
	if id := resp.Schema.Attributes["id"]; !id.IsComputed() || id.IsOptional() {
		t.Errorf("id must be Computed and not Optional")
	}
}
