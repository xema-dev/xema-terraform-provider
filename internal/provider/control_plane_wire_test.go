// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// wireOrgRoot stands in for the org-root scope ref the control plane forms with
// formatSpaceRef({ tier: Org, orgId }) when a binding names no scopeRef.
const wireOrgRoot = "space:org/org-1"

// fakeWireControlPlane answers as the control plane's role and role-assignment
// adapters do (xema-base biomes/control-plane/api/control-plane-api/src/adapters
// role.adapter.ts and role-assignment.adapter.ts, unit
// opus-s2-6-controlplane-bindings at 6fdb855b), because no landed control plane
// speaks that shape yet. It follows them where a provider can tell:
//   - role: spec is exactly { slug, displayName, description?, pattern }; `grants`
//     and `assignments` are refused 400 ROLE_SPEC_FIELD_RETIRED; slug and
//     displayName are non-empty strings and pattern an object; a null
//     description reads back absent.
//   - role-assignment: spec is exactly { definitionId, principal{kind, ref},
//     scopeRef?, environmentRef?, until? }; roleId, subjectKind and subjectRef are
//     refused 400 ROLE_ASSIGNMENT_SPEC_FIELD_RETIRED; the read-back always states
//     scopeRef (the org root when omitted), drops an absent environmentRef and
//     `until`, and returns `until` as the ISO string (Date#toISOString).
//   - the physical id is the definition id / the binding id; an update of an
//     assignment is refused 422.
type wireControlPlane struct {
	*httptest.Server
	roles       map[string]map[string]any
	assignments map[string]map[string]any
}

func refuse(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message})
}

func specKeysOutside(spec map[string]any, allowed ...string) []string {
	var extra []string
	for k := range spec {
		ok := false
		for _, a := range allowed {
			ok = ok || a == k
		}
		if !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return extra
}

func nonEmptyString(v any) bool {
	s, ok := v.(string)
	return ok && s != ""
}

func newWireControlPlane(t *testing.T) *wireControlPlane {
	t.Helper()
	fake := &wireControlPlane{roles: map[string]map[string]any{}, assignments: map[string]map[string]any{}}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Spec map[string]any `json:"spec"`
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		const roleBase, assignBase = "/control-plane/resources/role", "/control-plane/resources/role-assignment"
		switch {
		case r.Method == http.MethodPost && r.URL.Path == roleBase:
			spec := body.Spec
			for _, retired := range []string{"grants", "assignments"} {
				if _, ok := spec[retired]; ok {
					refuse(w, http.StatusBadRequest, "ROLE_SPEC_FIELD_RETIRED", retired+" was removed from the role declaration")
					return
				}
			}
			_, patternIsObject := spec["pattern"].(map[string]any)
			if extra := specKeysOutside(spec, "slug", "displayName", "description", "pattern"); len(extra) > 0 ||
				!nonEmptyString(spec["slug"]) || !nonEmptyString(spec["displayName"]) || !patternIsObject {
				refuse(w, http.StatusUnprocessableEntity, "RESOURCE_SPEC_INVALID", fmt.Sprintf("role spec refused: %v", spec))
				return
			}
			if d, ok := spec["description"]; ok {
				if _, isString := d.(string); !isString {
					refuse(w, http.StatusUnprocessableEntity, "RESOURCE_SPEC_INVALID", "role description must be a string")
					return
				}
			}
			id := "def-" + spec["slug"].(string)
			fake.roles[id] = spec
			w.WriteHeader(http.StatusCreated)
			writeData(w, map[string]any{"kind": "role", "physicalId": id, "managedKey": spec["slug"]})
		case strings.HasPrefix(r.URL.Path, roleBase+"/") && r.Method == http.MethodGet:
			id := strings.TrimPrefix(r.URL.Path, roleBase+"/")
			spec, ok := fake.roles[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			writeData(w, map[string]any{"kind": "role", "physicalId": id, "managedKey": spec["slug"], "spec": spec})
		case r.Method == http.MethodPost && r.URL.Path == assignBase:
			spec := body.Spec
			for _, retired := range []string{"roleId", "subjectKind", "subjectRef"} {
				if _, ok := spec[retired]; ok {
					refuse(w, http.StatusBadRequest, "ROLE_ASSIGNMENT_SPEC_FIELD_RETIRED", retired+" was replaced in the role-assignment declaration")
					return
				}
			}
			principal, _ := spec["principal"].(map[string]any)
			if extra := specKeysOutside(spec, "definitionId", "principal", "scopeRef", "environmentRef", "until"); len(extra) > 0 ||
				!nonEmptyString(spec["definitionId"]) || principal == nil ||
				!nonEmptyString(principal["kind"]) || !nonEmptyString(principal["ref"]) {
				refuse(w, http.StatusUnprocessableEntity, "RESOURCE_SPEC_INVALID", fmt.Sprintf("role-assignment spec refused: %v", spec))
				return
			}
			if _, ok := fake.roles[spec["definitionId"].(string)]; !ok {
				refuse(w, http.StatusUnprocessableEntity, "RESOURCE_SPEC_INVALID", "unknown role definition")
				return
			}
			held := map[string]any{
				"definitionId": spec["definitionId"],
				"principal":    map[string]any{"kind": principal["kind"], "ref": principal["ref"]},
				"scopeRef":     wireOrgRoot,
			}
			if v, ok := spec["scopeRef"]; ok {
				held["scopeRef"] = v
			}
			if v, ok := spec["environmentRef"]; ok {
				held["environmentRef"] = v
			}
			if v, ok := spec["until"].(string); ok {
				at, err := time.Parse(time.RFC3339, v)
				if err != nil {
					refuse(w, http.StatusUnprocessableEntity, "RESOURCE_SPEC_INVALID", "`until` must be a date-time.")
					return
				}
				held["until"] = at.UTC().Format("2006-01-02T15:04:05.000Z")
			}
			id := fmt.Sprintf("binding-%d", len(fake.assignments)+1)
			fake.assignments[id] = held
			w.WriteHeader(http.StatusCreated)
			writeData(w, map[string]any{"kind": "role-assignment", "physicalId": id, "managedKey": id})
		case strings.HasPrefix(r.URL.Path, assignBase+"/") && r.Method == http.MethodGet:
			id := strings.TrimPrefix(r.URL.Path, assignBase+"/")
			spec, ok := fake.assignments[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			writeData(w, map[string]any{"kind": "role-assignment", "physicalId": id, "managedKey": id, "spec": spec})
		case strings.HasPrefix(r.URL.Path, assignBase+"/") && r.Method == http.MethodPut:
			refuse(w, http.StatusUnprocessableEntity, "RESOURCE_SPEC_FIELD_IMMUTABLE", "a binding is immutable")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func TestRoleAndAssignmentAgainstTheControlPlaneWireShape(t *testing.T) {
	ctx := context.Background()
	fake := newWireControlPlane(t)
	c := client.New(fake.URL, "org-1", "tok", fake.Client())
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	null := tftypes.NewValue(tftypes.String, nil)

	// xema_role with a pattern.
	const declared = `{"clauses":[{"actions":["authorization:installation-consent.decide@1"],"where":{"env":"org"}}]}`
	role := &roleResource{client: c}
	var rsr resource.SchemaResponse
	role.Schema(ctx, resource.SchemaRequest{}, &rsr)
	roleObj := rsr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	rolePlanRaw := tftypes.NewValue(roleObj, map[string]tftypes.Value{
		"id": unknown, "slug": str("consent-deciders"), "display_name": str("Consent deciders"),
		"description": unknown, "pattern": str(declared),
	})
	roleResp := resource.CreateResponse{State: tfsdk.State{Schema: rsr.Schema, Raw: tftypes.NewValue(roleObj, nil)}}
	role.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: rsr.Schema, Raw: rolePlanRaw}}, &roleResp)
	if roleResp.Diagnostics.HasError() {
		t.Fatalf("create role: %v", roleResp.Diagnostics)
	}
	var roleState roleModel
	if diags := roleResp.State.Get(ctx, &roleState); diags.HasError() {
		t.Fatalf("role state: %v", diags)
	}
	if roleState.ID.ValueString() != "def-consent-deciders" {
		t.Fatalf("role id = %v", roleState.ID)
	}
	if eq, _ := roleState.Pattern.StringSemanticEquals(ctx, jsontypes.NewNormalizedValue(declared)); !eq {
		t.Errorf("role pattern read back as %s, declared %s", roleState.Pattern, declared)
	}
	if !roleState.Description.IsNull() {
		t.Errorf("an absent description reads back null, got %v", roleState.Description)
	}
	roleRead := resource.ReadResponse{State: roleResp.State}
	role.Read(ctx, resource.ReadRequest{State: roleResp.State}, &roleRead)
	if roleRead.Diagnostics.HasError() || !roleRead.State.Raw.Equal(roleResp.State.Raw) {
		t.Errorf("the role plan after apply must be empty: read %v", roleRead.Diagnostics)
	}

	// xema_role_assignment: definition_id from the role, a principal, no scope, an `until`.
	assign := &roleAssignmentResource{client: c}
	var asr resource.SchemaResponse
	assign.Schema(ctx, resource.SchemaRequest{}, &asr)
	assignObj := asr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	principalType := assignObj.AttributeTypes["principal"]
	const until = "2030-01-01T00:00:00Z"
	assignPlanRaw := tftypes.NewValue(assignObj, map[string]tftypes.Value{
		"id": unknown, "definition_id": str(roleState.ID.ValueString()),
		"principal": tftypes.NewValue(principalType, map[string]tftypes.Value{
			"kind": str("service"), "ref": str("principal-1"),
		}),
		"scope_ref": unknown, "environment_ref": null, "until": str(until),
	})
	assignResp := resource.CreateResponse{State: tfsdk.State{Schema: asr.Schema, Raw: tftypes.NewValue(assignObj, nil)}}
	assign.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: asr.Schema, Raw: assignPlanRaw}}, &assignResp)
	if assignResp.Diagnostics.HasError() {
		t.Fatalf("create role assignment: %v", assignResp.Diagnostics)
	}
	var got roleAssignmentModel
	if diags := assignResp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("assignment state: %v", diags)
	}
	if got.ID.ValueString() != "binding-1" || got.DefinitionID.ValueString() != "def-consent-deciders" ||
		got.Principal == nil || got.Principal.Kind.ValueString() != "service" || got.Principal.Ref.ValueString() != "principal-1" {
		t.Errorf("unexpected assignment state: %#v", got)
	}
	if got.ScopeRef.ValueString() != wireOrgRoot {
		t.Errorf("an omitted scope reads back as the org root %q, got %q", wireOrgRoot, got.ScopeRef.ValueString())
	}
	if !got.EnvironmentRef.IsNull() {
		t.Errorf("an absent environmentRef reads back null, got %v", got.EnvironmentRef)
	}
	if fake.assignments["binding-1"]["until"] != "2030-01-01T00:00:00.000Z" {
		t.Errorf("the control plane holds until as %v", fake.assignments["binding-1"]["until"])
	}
	if got.Until.ValueString() != until {
		t.Errorf("until must round-trip as declared, got %q", got.Until.ValueString())
	}

	// Plan after apply is empty: a refresh leaves the state as it is, and the
	// state keeps every value the plan fixed.
	assignRead := resource.ReadResponse{State: assignResp.State}
	assign.Read(ctx, resource.ReadRequest{State: assignResp.State}, &assignRead)
	if assignRead.Diagnostics.HasError() || !assignRead.State.Raw.Equal(assignResp.State.Raw) {
		t.Errorf("the assignment plan after apply must be empty: read %v", assignRead.Diagnostics)
	}
	var planned roleAssignmentModel
	if diags := (tfsdk.Plan{Schema: asr.Schema, Raw: assignPlanRaw}).Get(ctx, &planned); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	if planned.DefinitionID != got.DefinitionID || *planned.Principal != *got.Principal ||
		planned.Until != got.Until || planned.EnvironmentRef != got.EnvironmentRef {
		t.Errorf("state differs from the plan: plan %#v state %#v", planned, got)
	}

	// The retired fields are refused by the control plane, so the provider must never send them.
	for _, spec := range []map[string]any{{"slug": "x", "displayName": "X", "pattern": map[string]any{}, "grants": []any{}}} {
		if _, err := c.Create(ctx, roleKind, spec); err == nil {
			t.Error("the fake control plane must refuse grants, as the adapter does")
		}
	}
}
