// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

const rolePatternJSON = `{"clauses":[{"actions":["authorization:installation-consent.decide@1"]}]}`

type roleServer struct {
	*httptest.Server
	sent map[string]any
}

// fakeRoleControlPlane stores the spec it is sent and reads it back as the role
// kind does: { slug, displayName, description?, pattern }.
func fakeRoleControlPlane(t *testing.T) *roleServer {
	t.Helper()
	const base = "/control-plane/resources/role"
	fake := &roleServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			var body struct {
				Spec map[string]any `json:"spec"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fake.sent = body.Spec
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			w.WriteHeader(http.StatusCreated)
			writeData(w, map[string]any{"kind": "role", "physicalId": "role-1", "managedKey": "consent-deciders"})
		case r.URL.Path == base+"/role-1":
			writeData(w, map[string]any{"kind": "role", "physicalId": "role-1", "managedKey": "consent-deciders", "spec": fake.sent})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func rolePlan(sch schema.Schema, pattern string) (tftypes.Object, tftypes.Value) {
	obj := sch.Type().TerraformType(context.Background()).(tftypes.Object)
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	return obj, tftypes.NewValue(obj, map[string]tftypes.Value{
		"id":           tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"slug":         str("consent-deciders"),
		"display_name": str("Consent deciders"),
		"description":  tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"pattern":      str(pattern),
	})
}

func createRole(t *testing.T, fake *roleServer, pattern string) (*roleResource, resource.CreateResponse) {
	t.Helper()
	ctx := context.Background()
	r := &roleResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	obj, planRaw := rolePlan(sr.Schema, pattern)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: planRaw}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	return r, resp
}

func TestRoleCreateSendsThePattern(t *testing.T) {
	fake := fakeRoleControlPlane(t)
	_, resp := createRole(t, fake, rolePatternJSON)

	if _, ok := fake.sent["grants"]; ok {
		t.Errorf("grants is retired and must not be sent: %#v", fake.sent)
	}
	if fake.sent["slug"] != "consent-deciders" || fake.sent["displayName"] != "Consent deciders" {
		t.Errorf("unexpected spec: %#v", fake.sent)
	}
	pattern, ok := fake.sent["pattern"].(map[string]any)
	if !ok || len(pattern["clauses"].([]any)) != 1 {
		t.Errorf("pattern must be sent as the JSON object: %#v", fake.sent["pattern"])
	}
	var got roleModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if got.ID.ValueString() != "role-1" {
		t.Errorf("id = %v", got.ID)
	}
	equal, diags := got.Pattern.StringSemanticEquals(context.Background(), jsontypes.NewNormalizedValue(rolePatternJSON))
	if diags.HasError() || !equal {
		t.Errorf("pattern must read back as declared, got %s", got.Pattern)
	}
}

func TestRoleInvalidPatternIsRefusedBeforeAnyCall(t *testing.T) {
	m := roleModel{Pattern: jsontypes.NewNormalizedValue(`{not json`)}
	if _, err := m.toSpec(); err == nil {
		t.Error("a pattern that is not JSON must be refused")
	}
}

func TestRoleSchema(t *testing.T) {
	var sr resource.SchemaResponse
	(&roleResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sr)
	if _, ok := sr.Schema.Attributes["grants"]; ok {
		t.Error("grants is retired")
	}
	pattern, ok := sr.Schema.Attributes["pattern"].(schema.StringAttribute)
	if !ok || !pattern.IsRequired() {
		t.Error("pattern must be a required string")
	}
	slug, ok := sr.Schema.Attributes["slug"].(schema.StringAttribute)
	if !ok || len(slug.PlanModifiers) == 0 {
		t.Error("slug must force replacement")
	}
}
