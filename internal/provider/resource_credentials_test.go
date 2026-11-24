// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// seenCall is one request the fake control plane saw.
type seenCall struct {
	Method string
	Path   string
	Body   map[string]any
}

// fakeKindServer serves one kind's routes under the {"data": ...} envelope: it
// answers each call from respond and records every call.
type fakeKindServer struct {
	*httptest.Server
	mu    sync.Mutex
	calls []seenCall
}

func newFakeKindServer(t *testing.T, respond func(c seenCall, w http.ResponseWriter)) *fakeKindServer {
	t.Helper()
	f := &fakeKindServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := seenCall{Method: r.Method, Path: r.URL.EscapedPath()}
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			_ = json.NewDecoder(r.Body).Decode(&call.Body)
		}
		f.mu.Lock()
		f.calls = append(f.calls, call)
		f.mu.Unlock()
		respond(call, w)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeKindServer) called(method string) []seenCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []seenCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message})
}

func schemaOf(t *testing.T, r resource.Resource) schema.Schema {
	t.Helper()
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	if sr.Diagnostics.HasError() {
		t.Fatalf("schema: %v", sr.Diagnostics)
	}
	if diags := sr.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("schema implementation: %v", diags)
	}
	return sr.Schema
}

func stateFrom(t *testing.T, sch schema.Schema, model any) tfsdk.State {
	t.Helper()
	obj := sch.Type().TerraformType(context.Background()).(tftypes.Object)
	st := tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}
	if diags := st.Set(context.Background(), model); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	return st
}

// ---- org setting ----

func TestOrgSettingSpecRefusesANonScalarValue(t *testing.T) {
	m := orgSettingModel{Key: types.StringValue("ns:name@1"), Value: jsontypes.NewNormalizedValue(`{"a":1}`)}
	if _, err := m.toSpec(); err == nil {
		t.Fatal("an object value must be refused before any call")
	}
	m.Value = jsontypes.NewNormalizedValue(`"PT5M"`)
	spec, err := m.toSpec()
	if err != nil || spec["value"] != "PT5M" || spec["key"] != "ns:name@1" {
		t.Fatalf("spec = %v, err = %v", spec, err)
	}
}

func TestOrgSettingCreateReadDeleteClearsTheValue(t *testing.T) {
	ctx := context.Background()
	const key = "ns:name@1"
	srv := newFakeKindServer(t, func(c seenCall, w http.ResponseWriter) {
		switch {
		case c.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			writeData(w, map[string]any{"kind": "org-setting", "physicalId": key, "managedKey": key})
		case c.Method == http.MethodGet:
			writeData(w, map[string]any{
				"kind": "org-setting", "physicalId": key, "managedKey": key,
				"spec":   map[string]any{"key": key, "value": true},
				"status": map[string]any{"revision": 3},
			})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	r := &orgSettingResource{client: client.New(srv.URL, "org-1", "tok", srv.Client())}
	sch := schemaOf(t, r)
	plan := orgSettingModel{
		ID: types.StringUnknown(), Key: types.StringValue(key),
		Value: jsontypes.NewNormalizedValue("true"), Revision: types.Int64Unknown(),
	}
	obj := sch.Type().TerraformType(ctx).(tftypes.Object)
	createResp := resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sch, Raw: stateFrom(t, sch, plan).Raw}}, &createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("create: %v", createResp.Diagnostics)
	}
	var got orgSettingModel
	createResp.State.Get(ctx, &got)
	if got.ID.ValueString() != key || got.Revision.ValueInt64() != 3 || got.Value.ValueString() != "true" {
		t.Fatalf("state = %+v", got)
	}

	delResp := resource.DeleteResponse{}
	r.Delete(ctx, resource.DeleteRequest{State: createResp.State}, &delResp)
	if delResp.Diagnostics.HasError() {
		t.Fatalf("delete: %v", delResp.Diagnostics)
	}
	if n := len(srv.called(http.MethodDelete)); n != 1 {
		t.Fatalf("delete calls = %d, want 1", n)
	}
}

// ---- credential binding ----

func bindingModel(version, secret string) credentialBindingModel {
	return credentialBindingModel{
		ID:                  types.StringValue("cb-1"),
		ScopeTier:           types.StringValue("org"),
		ProjectID:           types.StringNull(),
		LLMProvider:         &llmProviderModel{Slug: types.StringValue("frontier"), BaseURL: types.StringValue("https://api.example.test/v1")},
		AuthType:            types.StringValue("bearer"),
		HeaderName:          types.StringNull(),
		AllowedCapabilities: strList("llm.invoke"),
		AllowedActions:      strList("invoke"),
		AllowedResources:    strList("*"),
		SecretValue:         types.StringValue(secret),
		SecretVersion:       types.StringValue(version),
		ProviderKey:         types.StringValue("k"),
		Version:             types.Int64Value(1),
	}
}

func TestCredentialBindingSchemaShape(t *testing.T) {
	sch := schemaOf(t, NewCredentialBindingResource())
	if !sch.Attributes["secret_value"].IsSensitive() {
		t.Error("`secret_value` must be sensitive")
	}
	for _, name := range []string{"scope_tier", "project_id", "auth_type", "llm_provider"} {
		if _, ok := sch.Attributes[name]; !ok {
			t.Errorf("schema is missing %s", name)
		}
	}
}

func TestCredentialBindingSpecSendsTheSecretOnlyWhenAsked(t *testing.T) {
	m := bindingModel("v1", "fake-secret")
	if _, ok := m.toSpec(false)["secretValue"]; ok {
		t.Error("secretValue must not be sent when withSecret is false")
	}
	if got := m.toSpec(true)["secretValue"]; got != "fake-secret" {
		t.Errorf("secretValue = %v", got)
	}
	if _, ok := m.toSpec(true)["llmProvider"]; !ok {
		t.Error("llmProvider must always be sent: the server needs it to check the provider key")
	}
}

func TestCredentialBindingUpdateRotatesOnlyWhenTheVersionChanges(t *testing.T) {
	ctx := context.Background()
	srv := newFakeKindServer(t, func(c seenCall, w http.ResponseWriter) {
		writeData(w, map[string]any{
			"kind": "credential-binding", "physicalId": "cb-1",
			"spec": map[string]any{
				"scopeTier": "org", "authType": "bearer",
				"allowedCapabilities": []any{"llm.invoke"}, "allowedActions": []any{"invoke"}, "allowedResources": []any{"*"},
			},
			"status": map[string]any{"providerKey": "k", "version": 2},
		})
	})
	r := &credentialBindingResource{client: client.New(srv.URL, "org-1", "tok", srv.Client())}
	sch := schemaOf(t, r)
	state := stateFrom(t, sch, bindingModel("v1", "fake-secret"))

	run := func(version string) {
		t.Helper()
		resp := resource.UpdateResponse{State: state}
		r.Update(ctx, resource.UpdateRequest{
			Plan:  tfsdk.Plan{Schema: sch, Raw: stateFrom(t, sch, bindingModel(version, "fake-secret")).Raw},
			State: state,
		}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("update: %v", resp.Diagnostics)
		}
	}
	run("v1")
	run("v2")
	puts := srv.called(http.MethodPut)
	if len(puts) != 2 {
		t.Fatalf("updates = %d, want 2", len(puts))
	}
	spec := func(c seenCall) map[string]any { s, _ := c.Body["spec"].(map[string]any); return s }
	if _, ok := spec(puts[0])["secretValue"]; ok {
		t.Error("an unchanged secret_version must not send the secret")
	}
	if spec(puts[1])["secretValue"] != "fake-secret" {
		t.Error("a changed secret_version must send the secret")
	}
}

func TestCredentialBindingDeleteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	srv := newFakeKindServer(t, func(c seenCall, w http.ResponseWriter) {
		writeAPIError(w, http.StatusNotFound, "RESOURCE_NOT_FOUND", "gone")
	})
	r := &credentialBindingResource{client: client.New(srv.URL, "org-1", "tok", srv.Client())}
	sch := schemaOf(t, r)
	resp := resource.DeleteResponse{}
	r.Delete(ctx, resource.DeleteRequest{State: stateFrom(t, sch, bindingModel("v1", "fake-secret"))}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("an already revoked or gone binding must destroy cleanly: %v", resp.Diagnostics)
	}
}

func TestCredentialBindingReadbackKeepsConfigOnlyFields(t *testing.T) {
	m := bindingModel("v1", "fake-secret")
	m.applyReadback(&client.Resource{PhysicalID: "cb-1", Spec: map[string]any{"scopeTier": "org", "authType": "bearer"}})
	if m.LLMProvider == nil || m.SecretValue.ValueString() != "fake-secret" || m.SecretVersion.ValueString() != "v1" {
		t.Errorf("read-back must not touch llm_provider, secret_value or secret_version: %+v", m)
	}
}

// ---- credential default ----

func TestCredentialDefaultCreateAndDelete(t *testing.T) {
	ctx := context.Background()
	const id = "org:frontier-key"
	srv := newFakeKindServer(t, func(c seenCall, w http.ResponseWriter) {
		switch c.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			writeData(w, map[string]any{"kind": "credential-default", "physicalId": id, "managedKey": id})
		case http.MethodGet:
			writeData(w, map[string]any{
				"kind": "credential-default", "physicalId": id,
				"spec":   map[string]any{"scopeTier": "org", "credentialBindingId": "cb-1"},
				"status": map[string]any{"providerKey": "frontier-key"},
			})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	r := &credentialDefaultResource{client: client.New(srv.URL, "org-1", "tok", srv.Client())}
	sch := schemaOf(t, r)
	plan := credentialDefaultModel{
		ID: types.StringUnknown(), ScopeTier: types.StringValue("org"), ProjectID: types.StringNull(),
		LLMProvider:         &llmProviderModel{Slug: types.StringValue("frontier"), BaseURL: types.StringValue("https://api.example.test/v1")},
		CredentialBindingID: types.StringValue("cb-1"), ProviderKey: types.StringUnknown(),
	}
	obj := sch.Type().TerraformType(ctx).(tftypes.Object)
	createResp := resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sch, Raw: stateFrom(t, sch, plan).Raw}}, &createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("create: %v", createResp.Diagnostics)
	}
	var got credentialDefaultModel
	createResp.State.Get(ctx, &got)
	if got.ID.ValueString() != id || got.ProviderKey.ValueString() != "frontier-key" || got.LLMProvider == nil {
		t.Fatalf("state = %+v", got)
	}
	posts := srv.called(http.MethodPost)
	if len(posts) != 1 || !strings.Contains(toJSON(posts[0].Body), `"llmProvider"`) {
		t.Fatalf("create body = %v", posts)
	}

	delResp := resource.DeleteResponse{}
	r.Delete(ctx, resource.DeleteRequest{State: createResp.State}, &delResp)
	if delResp.Diagnostics.HasError() || len(srv.called(http.MethodDelete)) != 1 {
		t.Fatalf("delete: %v", delResp.Diagnostics)
	}
}

func TestCredentialDefaultImportRefusesAnUnknownIDShape(t *testing.T) {
	r := &credentialDefaultResource{}
	sch := schemaOf(t, r)
	obj := sch.Type().TerraformType(context.Background()).(tftypes.Object)
	resp := resource.ImportStateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "nonsense"}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("an id that is neither org: nor project: must be refused")
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ---- egress destinations ----

func TestEgressDestinationsRevisionConflictIsAClearDiagnostic(t *testing.T) {
	ctx := context.Background()
	srv := newFakeKindServer(t, func(c seenCall, w http.ResponseWriter) {
		writeAPIError(w, http.StatusConflict, "EGRESS_DESTINATIONS_REVISION_CONFLICT", "moved")
	})
	r := &egressDestinationsResource{client: client.New(srv.URL, "org-1", "tok", srv.Client())}
	sch := schemaOf(t, r)
	plan := egressDestinationsModel{
		ID: types.StringUnknown(), Kind: types.StringValue("public-only"), Grants: jsontypes.NewNormalizedNull(),
		Revision: types.Int64Unknown(), Configured: types.BoolUnknown(),
	}
	obj := sch.Type().TerraformType(ctx).(tftypes.Object)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sch, Raw: stateFrom(t, sch, plan).Raw}}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("a conflict must fail the apply")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); got != "Egress destinations changed during apply" {
		t.Fatalf("summary = %q", got)
	}
}

func TestEgressDestinationsSpecIsThePolicyAlone(t *testing.T) {
	m := egressDestinationsModel{Kind: types.StringValue("grants"), Grants: jsontypes.NewNormalizedValue(`[{"host":"a.example.test"}]`)}
	spec, err := m.toSpec()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec["expectedRevision"]; ok || spec["kind"] != "grants" || spec["grants"] == nil {
		t.Fatalf("spec = %v", spec)
	}
	m.Grants = jsontypes.NewNormalizedValue(`{"a":1}`)
	if _, err := m.toSpec(); err == nil {
		t.Fatal("grants that are not a list must be refused before any call")
	}
}

func TestEgressDestinationsImportOnlyTheSingletonID(t *testing.T) {
	r := &egressDestinationsResource{}
	sch := schemaOf(t, r)
	obj := sch.Type().TerraformType(context.Background()).(tftypes.Object)
	resp := resource.ImportStateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "other"}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("only `installation` imports")
	}
}

// ---- deprecation ----

func TestProviderAPIKeyIsDeprecatedTowardsCredentialResources(t *testing.T) {
	sch := schemaOf(t, NewProviderResource())
	attr, ok := sch.Attributes["api_key"].(schema.StringAttribute)
	if !ok || !strings.Contains(attr.DeprecationMessage, "xema_credential_binding") ||
		!strings.Contains(attr.DeprecationMessage, "xema_credential_default") {
		t.Fatalf("api_key deprecation = %q", attr.DeprecationMessage)
	}
}
