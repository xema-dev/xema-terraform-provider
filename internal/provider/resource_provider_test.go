// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// The schema is the contract a practitioner reads. The platform refuses a
// credential reference on a provider row, so the resource must not offer one;
// `api_key` is the sole, write-only enrolment transport and must stay sensitive.
func TestProviderResourceSchemaCredentialShape(t *testing.T) {
	r := NewProviderResource()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)

	if _, ok := resp.Schema.Attributes["credential_ref"]; ok {
		t.Error("provider resource schema must not carry `credential_ref`")
	}
	apiKey, ok := resp.Schema.Attributes["api_key"]
	if !ok {
		t.Fatal("provider resource schema is missing `api_key`")
	}
	if !apiKey.IsSensitive() {
		t.Error("`api_key` must stay sensitive")
	}
}

func TestProviderSpecOmitsCredentialRef(t *testing.T) {
	spec := providerSpecModel{}.toSpec()
	if _, ok := spec["credentialRef"]; ok {
		t.Error("request spec must not carry credentialRef")
	}
}

// fakeProviderControlPlane serves the xema_provider routes in the real
// control-plane-api shape: the spec the caller sent is stored and read back, all
// under the {"data": ...} envelope.
func fakeProviderControlPlane(t *testing.T) *httptest.Server {
	t.Helper()
	const base = "/control-plane/resources/provider"
	spec := map[string]any{}
	resourceBody := func() map[string]any {
		return map[string]any{"kind": "provider", "physicalId": "prov-1", "managedKey": spec["slug"], "spec": spec}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			var body struct {
				Spec map[string]any `json:"spec"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			spec = body.Spec
			delete(spec, "apiKey") // the service never returns the secret
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			w.WriteHeader(http.StatusCreated)
			writeData(w, map[string]any{"kind": "provider", "physicalId": "prov-1", "managedKey": spec["slug"]})
		case r.URL.Path == base+"/prov-1":
			writeData(w, resourceBody())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func providerObject(t *testing.T, sch schema.Schema, id, name string) (tftypes.Object, tftypes.Value) {
	t.Helper()
	obj := sch.Type().TerraformType(context.Background()).(tftypes.Object)
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	idVal := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	if id != "" {
		idVal = str(id)
	}
	return obj, tftypes.NewValue(obj, map[string]tftypes.Value{
		"id":                      idVal,
		"name":                    str(name),
		"slug":                    str("frontier"),
		"api_type":                str("openai"),
		"base_url":                str("https://api.example.test/v1"),
		"auth_type":               str("bearer"),
		"api_key":                 tftypes.NewValue(tftypes.String, nil),
		"max_concurrent_requests": tftypes.NewValue(tftypes.Number, 4),
		"is_active":               tftypes.NewValue(tftypes.Bool, true),
	})
}

func assertProviderState(t *testing.T, step string, state tfsdk.State, wantName string) {
	t.Helper()
	var got providerSpecModel
	if diags := state.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("%s: state: %v", step, diags)
	}
	want := map[string]string{
		"id": "prov-1", "name": wantName, "slug": "frontier", "api_type": "openai",
		"base_url": "https://api.example.test/v1", "auth_type": "bearer",
	}
	have := map[string]string{
		"id": got.ID.ValueString(), "name": got.Name.ValueString(), "slug": got.Slug.ValueString(),
		"api_type": got.APIType.ValueString(), "base_url": got.BaseURL.ValueString(),
		"auth_type": got.AuthType.ValueString(),
	}
	for k, v := range want {
		if have[k] != v {
			t.Errorf("%s: %s = %q, want %q", step, k, have[k], v)
		}
	}
}

// create, read and update must fill every server-owned field from the enveloped
// response; an empty field here taints the state ("inconsistent result after apply").
func TestProviderResourceCreateReadUpdateDecodeTheEnvelope(t *testing.T) {
	ctx := context.Background()
	srv := fakeProviderControlPlane(t)
	r := &providerResource{client: client.New(srv.URL, "org-1", "tok", srv.Client())}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	obj, planRaw := providerObject(t, sr.Schema, "", "Frontier")
	emptyState := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(obj, nil)}

	createResp := resource.CreateResponse{State: emptyState}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: planRaw}}, &createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("create: %v", createResp.Diagnostics)
	}
	assertProviderState(t, "create", createResp.State, "Frontier")

	readResp := resource.ReadResponse{State: createResp.State}
	r.Read(ctx, resource.ReadRequest{State: createResp.State}, &readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("read: %v", readResp.Diagnostics)
	}
	assertProviderState(t, "read", readResp.State, "Frontier")

	_, updRaw := providerObject(t, sr.Schema, "prov-1", "Frontier Two")
	updResp := resource.UpdateResponse{State: readResp.State}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: sr.Schema, Raw: updRaw},
		State: readResp.State,
	}, &updResp)
	if updResp.Diagnostics.HasError() {
		t.Fatalf("update: %v", updResp.Diagnostics)
	}
	assertProviderState(t, "update", updResp.State, "Frontier Two")
}
