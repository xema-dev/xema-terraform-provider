// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

type roleAssignmentServer struct {
	*httptest.Server
	sent map[string]any
	// held is the assignment the server holds, by physical id; nil once withdrawn.
	held map[string]map[string]any
	// paths are the escaped request paths, in order.
	paths   []string
	deletes int
}

// fakeRoleAssignmentControlPlane answers as the role-assignment kind does: the
// physical id is the binding id, an absent assignment
// reads 404, and so does deleting one that is gone.
func fakeRoleAssignmentControlPlane(t *testing.T) *roleAssignmentServer {
	t.Helper()
	const base = "/control-plane/resources/role-assignment"
	fake := &roleAssignmentServer{held: map[string]map[string]any{}}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.paths = append(fake.paths, r.URL.EscapedPath())
		id := strings.TrimPrefix(r.URL.Path, base+"/")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			var body struct {
				Spec map[string]any `json:"spec"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fake.sent = body.Spec
			physicalID := "binding-" + body.Spec["principal"].(map[string]any)["ref"].(string)
			held := map[string]any{}
			for k, v := range body.Spec {
				held[k] = v
			}
			held["scopeRef"] = "org-root"
			if until, ok := held["until"].(string); ok {
				held["until"] = strings.Replace(until, "Z", ".000Z", 1)
			}
			fake.held[physicalID] = held
			w.WriteHeader(http.StatusCreated)
			writeData(w, map[string]any{"kind": "role-assignment", "physicalId": physicalID, "managedKey": physicalID})
		case r.Method == http.MethodDelete:
			fake.deletes++
			if fake.held[id] == nil {
				http.NotFound(w, r)
				return
			}
			delete(fake.held, id)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && fake.held[id] != nil:
			writeData(w, map[string]any{"kind": "role-assignment", "physicalId": id, "managedKey": id, "spec": fake.held[id]})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func roleAssignmentSchema(t *testing.T, r *roleAssignmentResource) (schema.Schema, tftypes.Object) {
	t.Helper()
	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	return sr.Schema, sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
}

func createRoleAssignment(t *testing.T, fake *roleAssignmentServer, principalRef string, until tftypes.Value) (*roleAssignmentResource, resource.CreateResponse) {
	t.Helper()
	ctx := context.Background()
	r := &roleAssignmentResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	sch, obj := roleAssignmentSchema(t, r)
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	principalType := obj.AttributeTypes["principal"]
	plan := tftypes.NewValue(obj, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"definition_id": str("ckrole1"),
		"principal": tftypes.NewValue(principalType, map[string]tftypes.Value{
			"kind": str("service"),
			"ref":  str(principalRef),
		}),
		"scope_ref":       tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"environment_ref": tftypes.NewValue(tftypes.String, nil),
		"until":           until,
	})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sch, Raw: plan}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	return r, resp
}

func noUntil() tftypes.Value { return tftypes.NewValue(tftypes.String, nil) }

func TestRoleAssignmentCreateSendsTheBindingSpec(t *testing.T) {
	fake := fakeRoleAssignmentControlPlane(t)
	_, resp := createRoleAssignment(t, fake, "principal-1", noUntil())

	for _, retired := range []string{"roleId", "subjectKind", "subjectRef", "scopeRef", "environmentRef", "until"} {
		if _, ok := fake.sent[retired]; ok {
			t.Errorf("%s must not be sent: %#v", retired, fake.sent)
		}
	}
	if fake.sent["definitionId"] != "ckrole1" {
		t.Errorf("definitionId = %v", fake.sent["definitionId"])
	}
	principal, _ := fake.sent["principal"].(map[string]any)
	if principal["kind"] != "service" || principal["ref"] != "principal-1" {
		t.Errorf("principal = %#v", fake.sent["principal"])
	}
	var got roleAssignmentModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if got.ID.ValueString() != "binding-principal-1" || got.Principal.Ref.ValueString() != "principal-1" ||
		got.ScopeRef.ValueString() != "org-root" {
		t.Errorf("unexpected state: %#v", got)
	}
}

func TestRoleAssignmentUntilKeepsTheDeclaredForm(t *testing.T) {
	fake := fakeRoleAssignmentControlPlane(t)
	_, resp := createRoleAssignment(t, fake, "principal-1", tftypes.NewValue(tftypes.String, "2030-01-01T00:00:00Z"))

	var got roleAssignmentModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if got.Until.ValueString() != "2030-01-01T00:00:00Z" {
		t.Errorf("until = %q, want the declared form", got.Until.ValueString())
	}
}

func TestRoleAssignmentImportReadsTheServer(t *testing.T) {
	fake := fakeRoleAssignmentControlPlane(t)
	fake.held["binding-1"] = map[string]any{
		"definitionId": "ckrole1",
		"principal":    map[string]any{"kind": "service", "ref": "principal-1"},
		"scopeRef":     "org-root",
		"until":        "2030-01-01T00:00:00.000Z",
	}
	ctx := context.Background()
	r := &roleAssignmentResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	sch, obj := roleAssignmentSchema(t, r)

	importResp := resource.ImportStateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "binding-1"}, &importResp)
	if importResp.Diagnostics.HasError() {
		t.Fatalf("import: %v", importResp.Diagnostics)
	}
	readResp := resource.ReadResponse{State: importResp.State}
	r.Read(ctx, resource.ReadRequest{State: importResp.State}, &readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("read: %v", readResp.Diagnostics)
	}
	var got roleAssignmentModel
	if diags := readResp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if got.DefinitionID.ValueString() != "ckrole1" || got.Principal.Kind.ValueString() != "service" ||
		got.Principal.Ref.ValueString() != "principal-1" || got.Until.ValueString() != "2030-01-01T00:00:00.000Z" {
		t.Errorf("unexpected state: %#v", got)
	}
}

func TestRoleAssignmentGoneReadsAsRemoved(t *testing.T) {
	fake := fakeRoleAssignmentControlPlane(t)
	r, createResp := createRoleAssignment(t, fake, "principal-1", noUntil())
	fake.held = map[string]map[string]any{}

	readResp := resource.ReadResponse{State: createResp.State}
	r.Read(context.Background(), resource.ReadRequest{State: createResp.State}, &readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("read: %v", readResp.Diagnostics)
	}
	if !readResp.State.Raw.IsNull() {
		t.Error("an assignment that is gone must leave state")
	}
}

func TestRoleAssignmentDeleteIsIdempotent(t *testing.T) {
	fake := fakeRoleAssignmentControlPlane(t)
	r, createResp := createRoleAssignment(t, fake, "principal-1", noUntil())

	for i := 0; i < 2; i++ {
		delResp := resource.DeleteResponse{State: createResp.State}
		r.Delete(context.Background(), resource.DeleteRequest{State: createResp.State}, &delResp)
		if delResp.Diagnostics.HasError() {
			t.Fatalf("delete %d: %v", i, delResp.Diagnostics)
		}
	}
	if fake.deletes != 2 || len(fake.held) != 0 {
		t.Errorf("deletes = %d, held = %#v", fake.deletes, fake.held)
	}
}

func TestRoleAssignmentUpdateIsRefused(t *testing.T) {
	var resp resource.UpdateResponse
	(&roleAssignmentResource{}).Update(context.Background(), resource.UpdateRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Error("an assignment has no update")
	}
}

func TestRoleAssignmentSchema(t *testing.T) {
	sch, _ := roleAssignmentSchema(t, &roleAssignmentResource{})
	for _, retired := range []string{"role_id", "subject_kind", "subject_ref"} {
		if _, ok := sch.Attributes[retired]; ok {
			t.Errorf("%s is retired", retired)
		}
	}
	for _, name := range []string{"definition_id", "environment_ref", "until", "scope_ref"} {
		attr, ok := sch.Attributes[name].(schema.StringAttribute)
		if !ok || len(attr.PlanModifiers) == 0 {
			t.Errorf("%s must replace the resource", name)
		}
	}
	principal, ok := sch.Attributes["principal"].(schema.SingleNestedAttribute)
	if !ok || !principal.IsRequired() || len(principal.PlanModifiers) == 0 {
		t.Error("principal must be required and replace the resource")
	}
	if id := sch.Attributes["id"]; !id.IsComputed() || id.IsOptional() {
		t.Error("id must be Computed and not Optional")
	}
}
