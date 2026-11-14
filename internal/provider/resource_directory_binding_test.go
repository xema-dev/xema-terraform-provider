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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

type directoryBindingServer struct {
	*httptest.Server
	sent    map[string]any
	deleted bool
}

// fakeDirectoryBindingControlPlane stores the spec it is sent and reads it back
// the way identity-api does: an absent roleMap comes back as an empty list, and
// `authority` is reported by the server.
func fakeDirectoryBindingControlPlane(t *testing.T) *directoryBindingServer {
	t.Helper()
	const base = "/control-plane/resources/directory-binding"
	fake := &directoryBindingServer{}
	readSpec := func() map[string]any {
		out := map[string]any{"authority": "claims"}
		for k, v := range fake.sent {
			out[k] = v
		}
		if _, ok := out["roleMap"]; !ok {
			out["roleMap"] = []any{}
		}
		return out
	}
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
			writeData(w, map[string]any{"kind": "directory-binding", "physicalId": "org-1", "managedKey": "default"})
		case r.Method == http.MethodDelete && r.URL.Path == base+"/org-1":
			fake.deleted = true
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == base+"/org-1":
			writeData(w, map[string]any{"kind": "directory-binding", "physicalId": "org-1", "managedKey": "default", "spec": readSpec()})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func directoryBindingPlan(t *testing.T, sch schema.Schema, id string, withRoleMap bool) (tftypes.Object, tftypes.Value) {
	t.Helper()
	obj := sch.Type().TerraformType(context.Background()).(tftypes.Object)
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	unknown := func(t tftypes.Type) tftypes.Value { return tftypes.NewValue(t, tftypes.UnknownValue) }

	claimType := obj.AttributeTypes["member_claim"].(tftypes.Object)
	roleType := obj.AttributeTypes["role_map"].(tftypes.List)

	idVal := unknown(tftypes.String)
	if id != "" {
		idVal = str(id)
	}
	roleMap := unknown(roleType)
	if withRoleMap {
		roleMap = tftypes.NewValue(roleType, []tftypes.Value{
			tftypes.NewValue(roleType.ElementType, map[string]tftypes.Value{
				"value": str("grp-1"), "org_role": str("org_admin"),
			}),
		})
	}
	return obj, tftypes.NewValue(obj, map[string]tftypes.Value{
		"id":                    idVal,
		"provider_ref":          str("entra:tenant-1"),
		"application_client_id": str("app-1"),
		"member_claim": tftypes.NewValue(claimType, map[string]tftypes.Value{
			"kind": str("GROUP"),
			"values": tftypes.NewValue(claimType.AttributeTypes["values"], []tftypes.Value{
				str("grp-1"),
			}),
		}),
		"role_map":           roleMap,
		"scim_credential_id": unknown(tftypes.String),
		"authority":          unknown(tftypes.String),
	})
}

func TestDirectoryBindingSpecIsCamelCase(t *testing.T) {
	fake := fakeDirectoryBindingControlPlane(t)
	ctx := context.Background()
	r := &directoryBindingResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	obj, planRaw := directoryBindingPlan(t, sr.Schema, "", true)

	resp := resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: planRaw}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}

	if fake.sent["providerRef"] != "entra:tenant-1" || fake.sent["applicationClientId"] != "app-1" {
		t.Errorf("unexpected scalar spec: %#v", fake.sent)
	}
	claim, _ := fake.sent["memberClaim"].(map[string]any)
	if claim["kind"] != "GROUP" || len(claim["values"].([]any)) != 1 {
		t.Errorf("unexpected memberClaim: %#v", claim)
	}
	roles, _ := fake.sent["roleMap"].([]any)
	if len(roles) != 1 || roles[0].(map[string]any)["orgRole"] != "org_admin" {
		t.Errorf("unexpected roleMap: %#v", fake.sent["roleMap"])
	}
	if _, ok := fake.sent["scimCredentialId"]; ok {
		t.Errorf("an unset scim_credential_id must be omitted: %#v", fake.sent)
	}

	var got directoryBindingModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if got.ID.ValueString() != "org-1" || got.Authority.ValueString() != "claims" || !got.SCIMCredentialID.IsNull() {
		t.Errorf("unexpected state: %#v", got)
	}
}

func TestDirectoryBindingEmptyRoleMapReadsAsNull(t *testing.T) {
	fake := fakeDirectoryBindingControlPlane(t)
	ctx := context.Background()
	r := &directoryBindingResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	obj, planRaw := directoryBindingPlan(t, sr.Schema, "", false)

	createResp := resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: planRaw}}, &createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("create: %v", createResp.Diagnostics)
	}
	if _, ok := fake.sent["roleMap"]; ok {
		t.Errorf("an unset role_map must be omitted: %#v", fake.sent)
	}

	readResp := resource.ReadResponse{State: createResp.State}
	r.Read(ctx, resource.ReadRequest{State: createResp.State}, &readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("read: %v", readResp.Diagnostics)
	}
	var got directoryBindingModel
	if diags := readResp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if !got.RoleMap.IsNull() {
		t.Errorf("the server's empty role map must read as null, got %v", got.RoleMap)
	}
}

func TestDirectoryBindingDeleteHitsDelete(t *testing.T) {
	fake := fakeDirectoryBindingControlPlane(t)
	ctx := context.Background()
	r := &directoryBindingResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	obj, planRaw := directoryBindingPlan(t, sr.Schema, "", true)

	createResp := resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: planRaw}}, &createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("create: %v", createResp.Diagnostics)
	}

	delResp := resource.DeleteResponse{State: createResp.State}
	r.Delete(ctx, resource.DeleteRequest{State: createResp.State}, &delResp)
	if delResp.Diagnostics.HasError() {
		t.Fatalf("delete: %v", delResp.Diagnostics)
	}
	if !fake.deleted {
		t.Error("delete did not reach DELETE")
	}
}

func TestDirectoryBindingEnumsAreValidated(t *testing.T) {
	check := func(v validator.String, in string) bool {
		var resp validator.StringResponse
		v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(in)}, &resp)
		return !resp.Diagnostics.HasError()
	}
	kinds := stringOneOfValidator{allowed: directoryMemberClaimKinds}
	roles := stringOneOfValidator{allowed: directoryOrgRoles}
	if !check(kinds, "GROUP") || !check(kinds, "APP_ROLE") || check(kinds, "group") {
		t.Error("member_claim.kind must accept GROUP and APP_ROLE only")
	}
	if !check(roles, "org_owner") || !check(roles, "org_admin") || !check(roles, "org_member") || check(roles, "admin") {
		t.Error("org_role must accept org_owner, org_admin, org_member only")
	}
}

func TestDirectoryBindingSchema(t *testing.T) {
	var resp resource.SchemaResponse
	(&directoryBindingResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	for _, name := range []string{"provider_ref", "application_client_id", "member_claim"} {
		if !resp.Schema.Attributes[name].IsRequired() {
			t.Errorf("%s is required", name)
		}
	}
	if id := resp.Schema.Attributes["id"]; !id.IsComputed() || id.IsOptional() {
		t.Errorf("id must be Computed and not Optional")
	}
	if a := resp.Schema.Attributes["authority"]; !a.IsComputed() || a.IsOptional() {
		t.Errorf("authority must be Computed and not Optional")
	}
}
