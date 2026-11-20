// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

const consentDecide = "authorization:installation-consent.decide@1"

type roleServer struct {
	*httptest.Server
	sent map[string]any
	// raiseApproval makes the fake store requiresApproval true on every grant,
	// as the service's delegation ceiling may.
	raiseApproval bool
	// liveGrants, when set, is what the server holds regardless of what it was sent.
	liveGrants []any
}

// fakeRoleControlPlane stores the spec it is sent and reads it back the way the
// role kind does: an empty grant set is absent, and every grant carries
// requiresApproval.
func fakeRoleControlPlane(t *testing.T) *roleServer {
	t.Helper()
	const base = "/control-plane/resources/role"
	fake := &roleServer{}
	readSpec := func() map[string]any {
		out := map[string]any{}
		for k, v := range fake.sent {
			if k != "grants" {
				out[k] = v
			}
		}
		grants := fake.liveGrants
		if grants == nil {
			sent, _ := fake.sent["grants"].([]any)
			for _, item := range sent {
				g := map[string]any{}
				for k, v := range item.(map[string]any) {
					g[k] = v
				}
				if _, ok := g["requiresApproval"]; !ok || fake.raiseApproval {
					g["requiresApproval"] = fake.raiseApproval
				}
				grants = append(grants, g)
			}
		}
		if len(grants) > 0 {
			out["grants"] = grants
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
			writeData(w, map[string]any{"kind": "role", "physicalId": "role-1", "managedKey": "consent-deciders"})
		case r.URL.Path == base+"/role-1":
			writeData(w, map[string]any{"kind": "role", "physicalId": "role-1", "managedKey": "consent-deciders", "spec": readSpec()})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

// rolePlan builds a role plan; grants nil means `grants` unset.
func rolePlan(t *testing.T, sch schema.Schema, grants []map[string]tftypes.Value) (tftypes.Object, tftypes.Value) {
	t.Helper()
	obj := sch.Type().TerraformType(context.Background()).(tftypes.Object)
	setType := obj.AttributeTypes["grants"].(tftypes.Set)
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

	grantsVal := tftypes.NewValue(setType, nil)
	if grants != nil {
		elems := make([]tftypes.Value, 0, len(grants))
		for _, g := range grants {
			elems = append(elems, tftypes.NewValue(setType.ElementType, g))
		}
		grantsVal = tftypes.NewValue(setType, elems)
	}
	return obj, tftypes.NewValue(obj, map[string]tftypes.Value{
		"id":           tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"slug":         str("consent-deciders"),
		"display_name": str("Consent deciders"),
		"description":  tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"grants":       grantsVal,
	})
}

func grantValue(capability, environment string, requiresApproval *bool, constraints string) map[string]tftypes.Value {
	approval := tftypes.NewValue(tftypes.Bool, nil)
	if requiresApproval != nil {
		approval = tftypes.NewValue(tftypes.Bool, *requiresApproval)
	}
	constraintsVal := tftypes.NewValue(tftypes.String, nil)
	if constraints != "" {
		constraintsVal = tftypes.NewValue(tftypes.String, constraints)
	}
	return map[string]tftypes.Value{
		"capability":        tftypes.NewValue(tftypes.String, capability),
		"environment":       tftypes.NewValue(tftypes.String, environment),
		"requires_approval": approval,
		"constraints":       constraintsVal,
		"where":             tftypes.NewValue(tftypes.String, nil),
	}
}

func createRole(t *testing.T, fake *roleServer, grants []map[string]tftypes.Value) (*roleResource, schema.Schema, resource.CreateResponse) {
	t.Helper()
	ctx := context.Background()
	r := &roleResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	obj, planRaw := rolePlan(t, sr.Schema, grants)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: planRaw}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	return r, sr.Schema, resp
}

func TestRoleUnsetGrantsAreSentAsTheEmptySet(t *testing.T) {
	fake := fakeRoleControlPlane(t)
	_, _, resp := createRole(t, fake, nil)

	grants, ok := fake.sent["grants"].([]any)
	if !ok || len(grants) != 0 {
		t.Fatalf("an unset grants must be sent as [], got %#v", fake.sent["grants"])
	}
	var got roleModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if got.Grants != nil {
		t.Errorf("an unset grants must stay unset, got %#v", got.Grants)
	}
}

func TestRoleUpdateResendsTheGrants(t *testing.T) {
	fake := fakeRoleControlPlane(t)
	ctx := context.Background()
	r, sch, createResp := createRole(t, fake, []map[string]tftypes.Value{grantValue(consentDecide, "org", nil, "")})
	fake.sent = nil

	updateResp := resource.UpdateResponse{State: createResp.State}
	plan := tfsdk.Plan{Schema: sch, Raw: createResp.State.Raw}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: createResp.State}, &updateResp)
	if updateResp.Diagnostics.HasError() {
		t.Fatalf("update: %v", updateResp.Diagnostics)
	}
	grants, _ := fake.sent["grants"].([]any)
	if len(grants) != 1 || grants[0].(map[string]any)["capability"] != consentDecide {
		t.Fatalf("an update must carry the declared grants, got %#v", fake.sent["grants"])
	}
}

func TestRoleGrantsReadBackAsDeclared(t *testing.T) {
	fake := fakeRoleControlPlane(t)
	fake.raiseApproval = true
	_, _, resp := createRole(t, fake, []map[string]tftypes.Value{
		grantValue(consentDecide, "org", nil, `{ "maxCallsPerHour": 10 }`),
	})

	sent, _ := fake.sent["grants"].([]any)
	if len(sent) != 1 {
		t.Fatalf("sent grants = %#v", fake.sent["grants"])
	}
	g := sent[0].(map[string]any)
	if g["capability"] != consentDecide || g["environment"] != "org" {
		t.Errorf("unexpected grant: %#v", g)
	}
	if _, ok := g["requiresApproval"]; ok {
		t.Errorf("an unset requires_approval must be omitted: %#v", g)
	}
	if c, _ := g["constraints"].(map[string]any); c["maxCallsPerHour"] != float64(10) {
		t.Errorf("constraints must be sent as an object: %#v", g["constraints"])
	}
	if _, ok := g["where"]; ok {
		t.Errorf("an unset where must be omitted: %#v", g)
	}

	var got roleModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if len(got.Grants) != 1 {
		t.Fatalf("state grants = %#v", got.Grants)
	}
	if !got.Grants[0].RequiresApproval.IsNull() {
		t.Errorf("a live true over an unset gate is no difference, got %v", got.Grants[0].RequiresApproval)
	}
	if got.Grants[0].Constraints.ValueString() != `{ "maxCallsPerHour": 10 }` {
		t.Errorf("equal constraints must keep the declared text, got %s", got.Grants[0].Constraints)
	}
}

func TestRoleImportReadsGrantsFromTheServer(t *testing.T) {
	fake := fakeRoleControlPlane(t)
	fake.sent = map[string]any{"slug": "consent-deciders", "displayName": "Consent deciders"}
	fake.liveGrants = []any{
		map[string]any{"capability": consentDecide, "environment": "org", "requiresApproval": false},
		map[string]any{"capability": "kb:page.read@1", "environment": "project", "requiresApproval": true, "where": map[string]any{"space": "a"}},
	}
	ctx := context.Background()
	r := &roleResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	obj := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)

	importResp := resource.ImportStateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(obj, nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "role-1"}, &importResp)
	if importResp.Diagnostics.HasError() {
		t.Fatalf("import: %v", importResp.Diagnostics)
	}
	readResp := resource.ReadResponse{State: importResp.State}
	r.Read(ctx, resource.ReadRequest{State: importResp.State}, &readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("read: %v", readResp.Diagnostics)
	}
	var got roleModel
	if diags := readResp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if got.Slug.ValueString() != "consent-deciders" || len(got.Grants) != 2 {
		t.Fatalf("unexpected state: %#v", got)
	}
	byCap := map[string]roleGrantModel{}
	for _, g := range got.Grants {
		byCap[g.Capability.ValueString()] = g
	}
	if g := byCap[consentDecide]; !g.RequiresApproval.IsNull() || !g.Where.IsNull() || g.Environment.ValueString() != "org" {
		t.Errorf("unexpected consent grant: %#v", g)
	}
	if g := byCap["kb:page.read@1"]; !g.RequiresApproval.ValueBool() || g.Where.ValueString() != `{"space":"a"}` {
		t.Errorf("unexpected scoped grant: %#v", g)
	}
	var id string
	if diags := readResp.State.GetAttribute(ctx, path.Root("id"), &id); diags.HasError() || id != "role-1" {
		t.Errorf("id = %q", id)
	}
}

func TestRoleGrantsFromSpec(t *testing.T) {
	declaredFalse := roleGrantModel{}
	declaredFalse.Capability = types.StringValue("c@1")
	declaredFalse.Environment = types.StringValue("org")
	declaredFalse.RequiresApproval = types.BoolValue(false)
	declaredTrue := declaredFalse
	declaredTrue.RequiresApproval = types.BoolValue(true)

	live := func(approval bool) map[string]any {
		return map[string]any{"grants": []any{
			map[string]any{"capability": "c@1", "environment": "org", "requiresApproval": approval},
		}}
	}
	if got := roleGrantsFromSpec(live(true), []roleGrantModel{declaredFalse}); got[0].RequiresApproval.ValueBool() {
		t.Error("a declared false over a live true is no difference")
	}
	if got := roleGrantsFromSpec(live(false), []roleGrantModel{declaredTrue}); got[0].RequiresApproval.ValueBool() {
		t.Error("a declared true over a live false is a difference")
	}
	if got := roleGrantsFromSpec(map[string]any{}, []roleGrantModel{}); got == nil || len(got) != 0 {
		t.Error("a declared empty set must read back as the empty set")
	}
	if got := roleGrantsFromSpec(map[string]any{}, []roleGrantModel{declaredTrue}); got != nil {
		t.Error("grants removed out of band must read as unset")
	}
}
