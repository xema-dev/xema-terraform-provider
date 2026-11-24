// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

func TestOrgSettingSchemaShape(t *testing.T) {
	sch := schemaOf(t, &orgSettingResource{})
	for _, name := range []string{"key", "value"} {
		if !sch.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	for _, name := range []string{"id", "revision"} {
		if !sch.Attributes[name].IsComputed() {
			t.Errorf("%s must be computed", name)
		}
	}
}

func TestOrgSettingReadbackMapsAndReadsDrift(t *testing.T) {
	m := orgSettingModel{Key: types.StringValue("ns:n@1"), Value: jsontypes.NewNormalizedValue(`"PT5M"`)}
	m.applyReadback(&client.Resource{
		PhysicalID: "ns:n@1",
		Spec:       map[string]any{"key": "ns:n@1", "value": "PT10M"},
		Status:     map[string]any{"revision": float64(4)},
	})
	if m.ID.ValueString() != "ns:n@1" || m.Value.ValueString() != `"PT10M"` || m.Revision.ValueInt64() != 4 {
		t.Fatalf("model = %+v", m)
	}
	// No status revision reads as null, and a missing spec key falls back to the id.
	m.applyReadback(&client.Resource{PhysicalID: "ns:x@2", Spec: map[string]any{"value": true}})
	if !m.Revision.IsNull() || m.Key.ValueString() != "ns:x@2" {
		t.Fatalf("model = %+v", m)
	}
}

func TestOrgSettingUpdateOmitsTheKeyFromTheBody(t *testing.T) {
	ctx := context.Background()
	const key = "ns:name@1"
	srv := newFakeKindServer(t, func(c seenCall, w http.ResponseWriter) {
		writeData(w, map[string]any{
			"kind": "org-setting", "physicalId": key, "managedKey": key,
			"spec":   map[string]any{"key": key, "value": 7},
			"status": map[string]any{"revision": 2},
		})
	})
	r := &orgSettingResource{client: client.New(srv.URL, "org-1", "tok", srv.Client())}
	sch := schemaOf(t, r)
	plan := orgSettingModel{
		ID: types.StringValue(key), Key: types.StringValue(key),
		Value: jsontypes.NewNormalizedValue("7"), Revision: types.Int64Unknown(),
	}
	raw := stateFrom(t, sch, plan).Raw
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: sch, Raw: raw}}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: sch, Raw: raw}, State: tfsdk.State{Schema: sch, Raw: raw}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	puts := srv.called(http.MethodPut)
	if len(puts) != 1 {
		t.Fatalf("puts = %v", puts)
	}
	spec, _ := puts[0].Body["spec"].(map[string]any)
	if spec["value"] != float64(7) {
		t.Fatalf("puts = %v", puts)
	}
	if _, ok := spec["key"]; ok {
		t.Fatalf("update body must not carry the key: %v", puts[0].Body)
	}
}

func TestEgressDestinationsSchemaShape(t *testing.T) {
	sch := schemaOf(t, &egressDestinationsResource{})
	if !sch.Attributes["kind"].IsRequired() || !sch.Attributes["grants"].IsOptional() {
		t.Fatal("kind is required and grants optional")
	}
	for _, name := range []string{"id", "revision", "configured"} {
		if !sch.Attributes[name].IsComputed() {
			t.Errorf("%s must be computed", name)
		}
	}
}

func TestEgressDestinationsReadbackMapsAndRefusesAnIncompleteStatus(t *testing.T) {
	var m egressDestinationsModel
	err := m.applyReadback(&client.Resource{
		Spec:   map[string]any{"kind": "grants", "grants": []any{map[string]any{"host": "a.example.test"}}},
		Status: map[string]any{"revision": float64(5), "configured": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID.ValueString() != "installation" || m.Kind.ValueString() != "grants" || m.Revision.ValueInt64() != 5 || !m.Configured.ValueBool() {
		t.Fatalf("model = %+v", m)
	}
	// Drift: the server dropped the grants, so they read as null.
	if err := m.applyReadback(&client.Resource{
		Spec:   map[string]any{"kind": "public-only"},
		Status: map[string]any{"revision": float64(6), "configured": true},
	}); err != nil || !m.Grants.IsNull() || m.Kind.ValueString() != "public-only" {
		t.Fatalf("err = %v, model = %+v", err, m)
	}
	if err := m.applyReadback(&client.Resource{Spec: map[string]any{"kind": "x"}}); err == nil {
		t.Fatal("a read-back without status must be refused")
	}
}

func TestEgressDestinationsReadRemovesAGoneResource(t *testing.T) {
	ctx := context.Background()
	srv := newFakeKindServer(t, func(c seenCall, w http.ResponseWriter) {
		writeAPIError(w, http.StatusNotFound, "NOT_FOUND", "gone")
	})
	r := &egressDestinationsResource{client: client.New(srv.URL, "org-1", "tok", srv.Client())}
	sch := schemaOf(t, r)
	st := stateFrom(t, sch, egressDestinationsModel{
		ID: types.StringValue("installation"), Kind: types.StringValue("public-only"),
		Grants: jsontypes.NewNormalizedNull(), Revision: types.Int64Value(1), Configured: types.BoolValue(true),
	})
	resp := resource.ReadResponse{State: st}
	r.Read(ctx, resource.ReadRequest{State: st}, &resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Fatalf("read: %v, raw null = %v", resp.Diagnostics, resp.State.Raw.IsNull())
	}
}

func TestCredentialDefaultReadbackReadsDrift(t *testing.T) {
	m := credentialDefaultModel{
		LLMProvider:         &llmProviderModel{Slug: types.StringValue("f"), BaseURL: types.StringValue("https://x.test")},
		CredentialBindingID: types.StringValue("cb-1"),
	}
	m.applyReadback(&client.Resource{
		PhysicalID: "org:k",
		Spec:       map[string]any{"scopeTier": "org", "credentialBindingId": "cb-2"},
		Status:     map[string]any{"providerKey": "k"},
	})
	if m.CredentialBindingID.ValueString() != "cb-2" || m.ProviderKey.ValueString() != "k" || !m.ProjectID.IsNull() || m.LLMProvider == nil {
		t.Fatalf("model = %+v", m)
	}
}
