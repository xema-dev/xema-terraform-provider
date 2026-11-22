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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
// physical id is `<roleId>:<subjectKind>:<subjectRef>`, an absent assignment
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
			physicalID := strings.Join([]string{
				body.Spec["roleId"].(string), body.Spec["subjectKind"].(string), body.Spec["subjectRef"].(string),
			}, ":")
			fake.held[physicalID] = body.Spec
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

func createRoleAssignment(t *testing.T, fake *roleAssignmentServer, subjectRef string) (*roleAssignmentResource, resource.CreateResponse) {
	t.Helper()
	ctx := context.Background()
	r := &roleAssignmentResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	sch, obj := roleAssignmentSchema(t, r)
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	plan := tftypes.NewValue(obj, map[string]tftypes.Value{
		"id":           tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"role_id":      str("ckrole1"),
		"subject_kind": str("service"),
		"subject_ref":  str(subjectRef),
	})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sch, Raw: plan}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	return r, resp
}

func TestRoleAssignmentCreateSendsTheKey(t *testing.T) {
	fake := fakeRoleAssignmentControlPlane(t)
	_, resp := createRoleAssignment(t, fake, "principal-1")

	want := map[string]any{"roleId": "ckrole1", "subjectKind": "service", "subjectRef": "principal-1"}
	for k, v := range want {
		if fake.sent[k] != v {
			t.Errorf("spec[%s] = %v, want %v", k, fake.sent[k], v)
		}
	}
	if len(fake.sent) != len(want) {
		t.Errorf("the spec carries only the key: %#v", fake.sent)
	}
	var got roleAssignmentModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if got.ID.ValueString() != "ckrole1:service:principal-1" || got.SubjectRef.ValueString() != "principal-1" {
		t.Errorf("unexpected state: %#v", got)
	}
}

func TestRoleAssignmentIdIsEscapedInThePath(t *testing.T) {
	fake := fakeRoleAssignmentControlPlane(t)
	createRoleAssignment(t, fake, "pipe/line 1")

	read := fake.paths[len(fake.paths)-1]
	if want := "/control-plane/resources/role-assignment/ckrole1:service:pipe%2Fline%201"; read != want {
		t.Errorf("read path = %q, want %q", read, want)
	}
}

func TestRoleAssignmentImportReadsTheServer(t *testing.T) {
	fake := fakeRoleAssignmentControlPlane(t)
	fake.held["ckrole1:service:principal-1"] = map[string]any{
		"roleId": "ckrole1", "subjectKind": "service", "subjectRef": "principal-1",
	}
	ctx := context.Background()
	r := &roleAssignmentResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	sch, obj := roleAssignmentSchema(t, r)

	importResp := resource.ImportStateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "ckrole1:service:principal-1"}, &importResp)
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
	if got.RoleID.ValueString() != "ckrole1" || got.SubjectKind.ValueString() != "service" || got.SubjectRef.ValueString() != "principal-1" {
		t.Errorf("unexpected state: %#v", got)
	}
}

func TestRoleAssignmentGoneReadsAsRemoved(t *testing.T) {
	fake := fakeRoleAssignmentControlPlane(t)
	r, createResp := createRoleAssignment(t, fake, "principal-1")
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
	r, createResp := createRoleAssignment(t, fake, "principal-1")

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
	for _, name := range []string{"role_id", "subject_kind", "subject_ref"} {
		attr, ok := sch.Attributes[name].(schema.StringAttribute)
		if !ok || !attr.IsRequired() || len(attr.PlanModifiers) == 0 {
			t.Errorf("%s must be required and replace the resource", name)
		}
	}
	if id := sch.Attributes["id"]; !id.IsComputed() || id.IsOptional() {
		t.Error("id must be Computed and not Optional")
	}

	check := func(in string) bool {
		var resp validator.StringResponse
		stringOneOfValidator{allowed: roleAssignmentSubjectKinds}.ValidateString(context.Background(),
			validator.StringRequest{ConfigValue: types.StringValue(in)}, &resp)
		return !resp.Diagnostics.HasError()
	}
	if !check("service") || !check("user") || check("svc") || check("Service") {
		t.Error("subject_kind must accept the SubjectKind values only")
	}
}
