// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// notServerFilled names the optional attributes the server never answers with a
// value of its own, each with the reason. Everything else optional is also
// computed, because a create or update reads the resource back: an attribute the
// configuration leaves null that the server then fills is otherwise refused by
// Terraform as an inconsistent result after apply.
var notServerFilled = map[string]string{
	"xema_provider.api_key":                              "write-only secret: the API never returns it",
	"xema_grant.resource_glob":                           "retired: the control plane refuses a declaration carrying it",
	"xema_portal.subdomain":                              "retired: the control plane refuses a declaration carrying it",
	"xema_model_resolution_rule.target_temperature":      "the rule's author sets it: the server stores what it is given, else null, and fills no default",
	"xema_model_resolution_rule.target_reasoning_effort": "the rule's author sets it: the server stores what it is given, else null, and fills no default",
}

func resourceSchemas(t *testing.T) map[string]schema.Schema {
	t.Helper()
	ctx := context.Background()
	out := map[string]schema.Schema{}
	for _, factory := range (&xemaProvider{}).Resources(ctx) {
		r := factory()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "xema"}, &meta)
		var sch resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sch)
		out[meta.TypeName] = sch.Schema
	}
	return out
}

func TestOptionalAttributesAcceptTheServersValue(t *testing.T) {
	for typeName, sch := range resourceSchemas(t) {
		for name, attr := range sch.Attributes {
			if !attr.IsOptional() || attr.IsComputed() {
				continue
			}
			if _, ok := notServerFilled[typeName+"."+name]; !ok {
				t.Errorf("%s.%s is optional and not computed, so a value the server fills fails the apply", typeName, name)
			}
		}
	}
}

func TestIdentityAttributesPlanAReplacement(t *testing.T) {
	identity := map[string][]string{
		"xema_team":        {"slug"},
		"xema_environment": {"slug", "kind"},
		"xema_grant":       {"subject_kind", "subject_ref", "capability"},
		"xema_role":        {"slug"},
		"xema_skill":       {"slug", "scope", "kind", "parent_slug"},
		"xema_portal":      {"slug"},
		"xema_model":       {"provider_id", "model_id"},
	}
	schemas := resourceSchemas(t)
	for typeName, names := range identity {
		for _, name := range names {
			attr, ok := schemas[typeName].Attributes[name].(schema.StringAttribute)
			if !ok {
				t.Errorf("%s.%s is not a string attribute", typeName, name)
				continue
			}
			if len(attr.PlanModifiers) == 0 {
				t.Errorf("%s.%s can change only by replacing the resource, and plans no replacement", typeName, name)
			}
		}
	}
}

func TestEnvironmentLeftUnsetTakesTheServersDefaults(t *testing.T) {
	ctx := context.Background()
	m := environmentModel{
		Slug:                types.StringValue("staging"),
		Kind:                types.StringValue("project"),
		Description:         types.StringUnknown(),
		AllowedCapabilities: types.ListUnknown(types.StringType),
		ApprovalRules:       jsontypes.NewNormalizedUnknown(),
		RuntimeLimits:       jsontypes.NewNormalizedUnknown(),
	}
	spec, err := m.toSpec(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"slug": "staging", "kind": "project"}; !reflect.DeepEqual(spec, want) {
		t.Fatalf("spec = %v, want %v", spec, want)
	}
	if err := m.applyReadback(ctx, map[string]any{
		"slug":          "staging",
		"kind":          "project",
		"approvalRules": []any{},
		"runtimeLimits": map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if m.RuntimeLimits.ValueString() != "{}" || m.ApprovalRules.ValueString() != "[]" {
		t.Fatalf("runtime_limits = %s, approval_rules = %s", m.RuntimeLimits, m.ApprovalRules)
	}
	if m.Description.IsUnknown() || m.AllowedCapabilities.IsUnknown() {
		t.Fatal("an attribute stayed unknown after the read-back")
	}
}

func TestModelLeftUnsetTakesTheServersCapabilities(t *testing.T) {
	ctx := context.Background()
	m := modelModel{
		ProviderID:    types.StringValue("p"),
		ModelID:       types.StringValue("m"),
		DisplayName:   types.StringValue("M"),
		ContextWindow: types.Int64Value(1),
		Capabilities:  types.ListUnknown(types.StringType),
		Modalities:    types.ListUnknown(types.StringType),
		IsActive:      types.BoolUnknown(),
	}
	spec, err := m.toSpec(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, sent := spec["capabilities"]; sent {
		t.Fatal("an unset capabilities was sent")
	}
	if err := m.applyReadback(ctx, map[string]any{
		"providerId":   "p",
		"modelId":      "m",
		"displayName":  "M",
		"capabilities": []any{"chat"},
		"modalities":   []any{},
		"isActive":     true,
	}); err != nil {
		t.Fatal(err)
	}
	if !m.Capabilities.Equal(strList("chat")) {
		t.Fatalf("capabilities = %s", m.Capabilities)
	}
	if !m.Modalities.IsNull() {
		t.Fatalf("modalities = %s, want null", m.Modalities)
	}
}

func TestGrantLeftUnsetTakesTheServersApprovalFlag(t *testing.T) {
	m := grantModel{
		SubjectKind:      types.StringValue("group"),
		SubjectRef:       types.StringValue("team-1"),
		Capability:       types.StringValue("kb:page.read@1"),
		Environment:      types.StringValue("org"),
		RequiresApproval: types.BoolUnknown(),
		Profile:          types.StringUnknown(),
		Constraints:      jsontypes.NewNormalizedUnknown(),
	}
	spec, err := m.toSpec()
	if err != nil {
		t.Fatal(err)
	}
	if _, sent := spec["requiresApproval"]; sent {
		t.Fatal("an unset requires_approval was sent")
	}
	m.applyReadback(map[string]any{"requiresApproval": false})
	if m.RequiresApproval.IsUnknown() || m.RequiresApproval.ValueBool() {
		t.Fatalf("requires_approval = %s, want false", m.RequiresApproval)
	}
}
