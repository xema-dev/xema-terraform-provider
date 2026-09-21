// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The credential form a provider declares decides WHERE the bytes live, and
// R-76 makes the reference form the one organisation configuration uses: a
// declarative client that had to carry a raw key would put it in a tfvars
// file, in a plan, and in the state backend.
//
// These assert the PLAN-time refusal. The service refuses the same two cases
// (a DTO validator and a CHECK constraint under it), so this is not the fence
// — it is the difference between an author seeing the problem in `tofu plan`
// and seeing it as an API error in the middle of an apply.

// Drive the REAL `ValidateConfig` through a real `tfsdk.Config` built from the
// resource's own schema. An earlier version of this helper re-implemented the
// two presence flags and the switch over them — which tests a copy of the rule
// and passes for ever after the rule itself changes. The whole point of this
// suite is that the resource refuses, so the resource is what it calls.
func validateProviderConfig(t *testing.T, apiKey, credentialRef tftypes.Value) resource.ValidateConfigResponse {
	t.Helper()

	r := NewProviderResource()
	var schemaResp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", schemaResp.Diagnostics)
	}

	objType := schemaResp.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, attrType := range objType.AttributeTypes {
		values[name] = tftypes.NewValue(attrType, nil)
	}
	values["api_key"] = apiKey
	values["credential_ref"] = credentialRef

	validator, ok := r.(resource.ResourceWithValidateConfig)
	if !ok {
		t.Fatal("provider resource does not implement ResourceWithValidateConfig")
	}

	var resp resource.ValidateConfigResponse
	validator.ValidateConfig(
		context.Background(),
		resource.ValidateConfigRequest{
			Config: tfsdk.Config{
				Schema: schemaResp.Schema,
				Raw:    tftypes.NewValue(objType, values),
			},
		},
		&resp,
	)
	return resp
}

func TestProviderResourceCredentialForms(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	null := tftypes.NewValue(tftypes.String, nil)
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)

	cases := []struct {
		name       string
		apiKey     tftypes.Value
		credRef    tftypes.Value
		wantsError bool
	}{
		{"credential_ref alone is the R-76 shape", null, str("cb-1"), false},
		{"api_key alone stays first-class", str("sk-1"), null, false},
		{"both is refused — two answers, no precedence", str("sk-1"), str("cb-1"), true},
		{"neither is refused — it would fail when an agent runs", null, null, true},
		// Unknown is NOT absent. A credential_ref produced by another resource
		// in the same apply is unknown at plan time, and refusing it would
		// break the ordinary case of creating the binding alongside the
		// provider.
		{"unknown credential_ref counts as present", null, unknown, false},
		{"unknown api_key counts as present", unknown, null, false},
		{"unknown on BOTH is still both", unknown, unknown, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := validateProviderConfig(t, tc.apiKey, tc.credRef)
			if got := resp.Diagnostics.HasError(); got != tc.wantsError {
				t.Fatalf("expected error=%v, got %v (%v)", tc.wantsError, got, resp.Diagnostics)
			}
		})
	}
}

// The schema is the contract a practitioner reads, so assert the attribute
// exists and is NOT marked sensitive: a credential_ref is an opaque pointer,
// not a secret, and marking it sensitive would hide the one field that makes a
// hand-switch to an inline key visible as drift.
func TestProviderResourceSchemaCarriesCredentialRef(t *testing.T) {
	r := NewProviderResource()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)

	attr, ok := resp.Schema.Attributes["credential_ref"]
	if !ok {
		t.Fatal("provider resource schema is missing `credential_ref`")
	}
	if attr.IsSensitive() {
		t.Error("`credential_ref` must NOT be sensitive — it is a pointer, and hiding it hides drift")
	}
	if attr.IsRequired() {
		t.Error("`credential_ref` must be optional — `api_key` is still a first-class form")
	}
	if apiKey := resp.Schema.Attributes["api_key"]; !apiKey.IsSensitive() {
		t.Error("`api_key` must stay sensitive")
	}
}
