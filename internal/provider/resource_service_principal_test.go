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

type servicePrincipalServer struct {
	*httptest.Server
	// omitSecret makes create and rotation answer without a clientSecret.
	omitSecret bool
	// held is the principal the server holds, by id; its state is held[id]["state"].
	held map[string]map[string]any
	sent map[string]any
	// rotationKeys and rotationBodies record each rotation request.
	rotationKeys   []string
	rotationBodies []map[string]any
	creates        int
	deletes        int
	secretOnRead   bool
}

// fakeServicePrincipalControlPlane answers as the service-principal kind does:
// create returns oneTimeOutputs once, read returns spec and status and never a
// secret, rotation answers a fresh secret.
func fakeServicePrincipalControlPlane(t *testing.T) *servicePrincipalServer {
	t.Helper()
	const base = "/control-plane/resources/service-principal"
	fake := &servicePrincipalServer{held: map[string]map[string]any{}}
	revision := 1
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, base)
		switch {
		case r.Method == http.MethodPost && rest == "":
			var body struct {
				Spec map[string]any `json:"spec"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fake.creates++
			fake.sent = body.Spec
			fake.held["sp-1"] = map[string]any{"spec": body.Spec, "state": "ACTIVE"}
			out := map[string]any{"kind": "service-principal", "physicalId": "sp-1", "managedKey": body.Spec["name"]}
			if !fake.omitSecret {
				out["oneTimeOutputs"] = map[string]string{
					"clientId": "cid-1", "clientSecret": "secret-1", "tokenEndpoint": "https://t/token", "credentialRevision": "1",
				}
			}
			w.WriteHeader(http.StatusCreated)
			writeData(w, out)
		case r.Method == http.MethodPost && strings.HasSuffix(rest, "/credential-rotations"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			fake.rotationKeys = append(fake.rotationKeys, r.Header.Get("Idempotency-Key"))
			fake.rotationBodies = append(fake.rotationBodies, body)
			revision++
			out := map[string]any{
				"credentialRevision": string(rune('0' + revision)), "clientId": "cid-1", "tokenEndpoint": "https://t/token", "replayed": false,
			}
			if !fake.omitSecret {
				out["clientSecret"] = "secret-" + string(rune('0'+revision))
			}
			writeData(w, out)
		case r.Method == http.MethodDelete:
			fake.deletes++
			id := strings.TrimPrefix(rest, "/")
			if fake.held[id] == nil {
				http.NotFound(w, r)
				return
			}
			delete(fake.held, id)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet:
			id := strings.TrimPrefix(rest, "/")
			p := fake.held[id]
			if p == nil {
				http.NotFound(w, r)
				return
			}
			status := map[string]any{
				"state": p["state"], "credentialRevision": string(rune('0' + revision)), "clientId": "cid-1", "tokenEndpoint": "https://t/token",
			}
			if fake.secretOnRead {
				status["clientSecret"] = "must-never-be-used"
			}
			writeData(w, map[string]any{"kind": "service-principal", "physicalId": id, "managedKey": "ci", "spec": p["spec"], "status": status})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func servicePrincipalSchema(t *testing.T, r *servicePrincipalResource) (schema.Schema, tftypes.Object) {
	t.Helper()
	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	return sr.Schema, sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
}

var spAttrs = []string{
	"id", "name", "display_name", "description", "rotation_trigger", "rotation_overlap_seconds",
	"client_id", "token_endpoint", "credential_revision", "state", "client_secret",
}

// spValue builds an object value with every attribute null unless overridden.
func spValue(obj tftypes.Object, set map[string]tftypes.Value) tftypes.Value {
	vals := map[string]tftypes.Value{}
	for _, a := range spAttrs {
		vals[a] = tftypes.NewValue(obj.AttributeTypes[a], nil)
	}
	for k, v := range set {
		vals[k] = v
	}
	return tftypes.NewValue(obj, vals)
}

func tfStr(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

func tfUnknown() tftypes.Value { return tftypes.NewValue(tftypes.String, tftypes.UnknownValue) }

func newServicePrincipalResource(fake *servicePrincipalServer) *servicePrincipalResource {
	return &servicePrincipalResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
}

func createServicePrincipal(t *testing.T, fake *servicePrincipalServer, trigger string) (*servicePrincipalResource, resource.CreateResponse) {
	t.Helper()
	r := newServicePrincipalResource(fake)
	sch, obj := servicePrincipalSchema(t, r)
	set := map[string]tftypes.Value{
		"id":                       tfUnknown(),
		"name":                     tfStr("ci"),
		"display_name":             tfStr("CI"),
		"rotation_overlap_seconds": tftypes.NewValue(tftypes.Number, 300),
		"client_id":                tfUnknown(),
		"token_endpoint":           tfUnknown(),
		"credential_revision":      tfUnknown(),
		"state":                    tfUnknown(),
		"client_secret":            tfUnknown(),
	}
	if trigger != "" {
		set["rotation_trigger"] = tfStr(trigger)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan{Schema: sch, Raw: spValue(obj, set)}}, &resp)
	return r, resp
}

func readSP(t *testing.T, state tfsdk.State) servicePrincipalModel {
	t.Helper()
	var got servicePrincipalModel
	if diags := state.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	return got
}

func TestServicePrincipalCreateStoresTheOneTimeSecret(t *testing.T) {
	fake := fakeServicePrincipalControlPlane(t)
	_, resp := createServicePrincipal(t, fake, "")
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	if fake.sent["name"] != "ci" || fake.sent["displayName"] != "CI" || len(fake.sent) != 2 {
		t.Errorf("spec = %#v", fake.sent)
	}
	got := readSP(t, resp.State)
	if got.ID.ValueString() != "sp-1" || got.ClientSecret.ValueString() != "secret-1" ||
		got.ClientID.ValueString() != "cid-1" || got.TokenEndpoint.ValueString() != "https://t/token" ||
		got.CredentialRevision.ValueString() != "1" || got.State.ValueString() != "ACTIVE" {
		t.Errorf("unexpected state: %#v", got)
	}
	if !got.Description.IsNull() {
		t.Errorf("description = %s, want null", got.Description)
	}
}

func TestServicePrincipalCreateWithoutSecretFails(t *testing.T) {
	fake := fakeServicePrincipalControlPlane(t)
	fake.omitSecret = true
	_, resp := createServicePrincipal(t, fake, "")
	if !resp.Diagnostics.HasError() {
		t.Fatal("a create with no clientSecret must fail, never store an empty secret")
	}
	if fake.deletes != 1 || len(fake.held) != 0 {
		t.Errorf("the untracked principal must be withdrawn: deletes=%d held=%v", fake.deletes, fake.held)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("no state may be saved for a create without a secret")
	}
}

func TestServicePrincipalReadNeverSetsTheSecret(t *testing.T) {
	fake := fakeServicePrincipalControlPlane(t)
	r, createResp := createServicePrincipal(t, fake, "")
	fake.secretOnRead = true // even a server that wrongly sends one must be ignored

	readResp := resource.ReadResponse{State: createResp.State}
	r.Read(context.Background(), resource.ReadRequest{State: createResp.State}, &readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("read: %v", readResp.Diagnostics)
	}
	if got := readSP(t, readResp.State); got.ClientSecret.ValueString() != "secret-1" {
		t.Errorf("client_secret = %s, want the stored secret-1", got.ClientSecret)
	}
}

func TestServicePrincipalReadRemovesGoneAndRevoked(t *testing.T) {
	fake := fakeServicePrincipalControlPlane(t)
	r, createResp := createServicePrincipal(t, fake, "")

	for _, state := range []string{"REVOKED", "REVOKING"} {
		fake.held["sp-1"]["state"] = state
		readResp := resource.ReadResponse{State: createResp.State}
		r.Read(context.Background(), resource.ReadRequest{State: createResp.State}, &readResp)
		if readResp.Diagnostics.HasError() || !readResp.State.Raw.IsNull() {
			t.Errorf("state %s must leave state: %v", state, readResp.Diagnostics)
		}
	}

	fake.held = map[string]map[string]any{}
	readResp := resource.ReadResponse{State: createResp.State}
	r.Read(context.Background(), resource.ReadRequest{State: createResp.State}, &readResp)
	if readResp.Diagnostics.HasError() || !readResp.State.Raw.IsNull() {
		t.Errorf("a 404 must leave state: %v", readResp.Diagnostics)
	}
}

func planModify(t *testing.T, r *servicePrincipalResource, state tfsdk.State, trigger string) servicePrincipalModel {
	t.Helper()
	sch, obj := servicePrincipalSchema(t, r)
	prior := readSP(t, state)
	set := map[string]tftypes.Value{
		"id":                       tfStr(prior.ID.ValueString()),
		"name":                     tfStr("ci"),
		"display_name":             tfStr("CI"),
		"rotation_overlap_seconds": tftypes.NewValue(tftypes.Number, 300),
		"client_id":                tfUnknown(),
		"token_endpoint":           tfStr(prior.TokenEndpoint.ValueString()),
		"credential_revision":      tfUnknown(),
		"state":                    tfStr(prior.State.ValueString()),
		"client_secret":            tfUnknown(),
	}
	if trigger != "" {
		set["rotation_trigger"] = tfStr(trigger)
	}
	plan := tfsdk.Plan{Schema: sch, Raw: spValue(obj, set)}
	resp := resource.ModifyPlanResponse{Plan: plan}
	r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: sch, Raw: plan.Raw}, Plan: plan, State: state,
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("modify plan: %v", resp.Diagnostics)
	}
	var got servicePrincipalModel
	if diags := resp.Plan.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	return got
}

func TestServicePrincipalReplanKeepsTheCredentialUnlessTheTriggerChanges(t *testing.T) {
	fake := fakeServicePrincipalControlPlane(t)
	r, createResp := createServicePrincipal(t, fake, "v1")

	same := planModify(t, r, createResp.State, "v1")
	if same.ClientSecret.ValueString() != "secret-1" || same.ClientID.ValueString() != "cid-1" ||
		same.CredentialRevision.ValueString() != "1" {
		t.Errorf("an unchanged trigger must keep the credential: %#v", same)
	}

	rotating := planModify(t, r, createResp.State, "v2")
	if !rotating.ClientSecret.IsUnknown() || !rotating.ClientID.IsUnknown() || !rotating.CredentialRevision.IsUnknown() {
		t.Errorf("a changed trigger must plan a new credential: %#v", rotating)
	}
}

func TestServicePrincipalReplanAfterImportIsStable(t *testing.T) {
	fake := fakeServicePrincipalControlPlane(t)
	fake.held["sp-1"] = map[string]any{"spec": map[string]any{"name": "ci", "displayName": "CI"}, "state": "ACTIVE"}
	r := newServicePrincipalResource(fake)
	sch, obj := servicePrincipalSchema(t, r)
	ctx := context.Background()

	importResp := resource.ImportStateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "sp-1"}, &importResp)
	if importResp.Diagnostics.HasError() {
		t.Fatalf("import: %v", importResp.Diagnostics)
	}
	readResp := resource.ReadResponse{State: importResp.State}
	r.Read(ctx, resource.ReadRequest{State: importResp.State}, &readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("read: %v", readResp.Diagnostics)
	}
	got := readSP(t, readResp.State)
	if !got.ClientSecret.IsNull() {
		t.Errorf("client_secret = %s, want null after import", got.ClientSecret)
	}
	if got.Name.ValueString() != "ci" || got.ClientID.ValueString() != "cid-1" {
		t.Errorf("unexpected state: %#v", got)
	}
	planned := planModify(t, r, readResp.State, "")
	if !planned.ClientSecret.IsNull() || planned.ClientID.IsUnknown() {
		t.Errorf("an imported principal must replan stable with a null secret: %#v", planned)
	}
}

func TestServicePrincipalTriggerChangeRotatesInPlace(t *testing.T) {
	fake := fakeServicePrincipalControlPlane(t)
	r, createResp := createServicePrincipal(t, fake, "v1")
	sch, obj := servicePrincipalSchema(t, r)

	planned := planModify(t, r, createResp.State, "v2")
	plan := tfsdk.Plan{Schema: sch, Raw: spValue(obj, map[string]tftypes.Value{
		"id":                       tfStr("sp-1"),
		"name":                     tfStr("ci"),
		"display_name":             tfStr("CI"),
		"rotation_trigger":         tfStr("v2"),
		"rotation_overlap_seconds": tftypes.NewValue(tftypes.Number, 600),
		"client_id":                tfUnknown(),
		"token_endpoint":           tfStr(planned.TokenEndpoint.ValueString()),
		"credential_revision":      tfUnknown(),
		"state":                    tfStr("ACTIVE"),
		"client_secret":            tfUnknown(),
	})}
	updResp := resource.UpdateResponse{State: createResp.State}
	r.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: createResp.State}, &updResp)
	if updResp.Diagnostics.HasError() {
		t.Fatalf("update: %v", updResp.Diagnostics)
	}

	if len(fake.rotationKeys) != 1 || fake.rotationKeys[0] == "" {
		t.Fatalf("rotation must send one Idempotency-Key: %v", fake.rotationKeys)
	}
	if fake.rotationBodies[0]["overlapSeconds"] != float64(600) {
		t.Errorf("rotation body = %#v", fake.rotationBodies[0])
	}
	if fake.creates != 1 || fake.deletes != 0 {
		t.Errorf("rotation must not replace: creates=%d deletes=%d", fake.creates, fake.deletes)
	}
	got := readSP(t, updResp.State)
	if got.ID.ValueString() != "sp-1" || got.ClientSecret.ValueString() != "secret-2" ||
		got.CredentialRevision.ValueString() != "2" || got.RotationTrigger.ValueString() != "v2" {
		t.Errorf("unexpected state: %#v", got)
	}

	// A second rotation uses a different key.
	updResp2 := resource.UpdateResponse{State: updResp.State}
	plan2 := tfsdk.Plan{Schema: sch, Raw: spValue(obj, map[string]tftypes.Value{
		"id": tfStr("sp-1"), "name": tfStr("ci"), "display_name": tfStr("CI"), "rotation_trigger": tfStr("v3"),
		"rotation_overlap_seconds": tftypes.NewValue(tftypes.Number, 600),
		"client_id":                tfUnknown(), "token_endpoint": tfStr("https://t/token"),
		"credential_revision": tfUnknown(), "state": tfStr("ACTIVE"), "client_secret": tfUnknown(),
	})}
	r.Update(context.Background(), resource.UpdateRequest{Plan: plan2, State: updResp.State}, &updResp2)
	if updResp2.Diagnostics.HasError() || len(fake.rotationKeys) != 2 || fake.rotationKeys[0] == fake.rotationKeys[1] {
		t.Errorf("each rotation needs its own idempotency key: %v %v", fake.rotationKeys, updResp2.Diagnostics)
	}
}

func TestServicePrincipalRotationWithoutSecretFails(t *testing.T) {
	fake := fakeServicePrincipalControlPlane(t)
	r, createResp := createServicePrincipal(t, fake, "v1")
	sch, obj := servicePrincipalSchema(t, r)
	fake.omitSecret = true

	plan := tfsdk.Plan{Schema: sch, Raw: spValue(obj, map[string]tftypes.Value{
		"id": tfStr("sp-1"), "name": tfStr("ci"), "display_name": tfStr("CI"), "rotation_trigger": tfStr("v2"),
		"rotation_overlap_seconds": tftypes.NewValue(tftypes.Number, 300),
		"client_id":                tfUnknown(), "token_endpoint": tfStr("https://t/token"),
		"credential_revision": tfUnknown(), "state": tfStr("ACTIVE"), "client_secret": tfUnknown(),
	})}
	updResp := resource.UpdateResponse{State: createResp.State}
	r.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: createResp.State}, &updResp)
	if !updResp.Diagnostics.HasError() {
		t.Fatal("a rotation with no clientSecret must fail")
	}
	if got := readSP(t, updResp.State); got.ClientSecret.ValueString() != "secret-1" || got.RotationTrigger.ValueString() != "v1" {
		t.Errorf("a failed rotation must leave state as it was: %#v", got)
	}
}

func TestServicePrincipalDeleteToleratesNotFound(t *testing.T) {
	fake := fakeServicePrincipalControlPlane(t)
	r, createResp := createServicePrincipal(t, fake, "")

	for i := 0; i < 2; i++ {
		delResp := resource.DeleteResponse{State: createResp.State}
		r.Delete(context.Background(), resource.DeleteRequest{State: createResp.State}, &delResp)
		if delResp.Diagnostics.HasError() {
			t.Fatalf("delete %d: %v", i, delResp.Diagnostics)
		}
	}
	if fake.deletes != 2 || len(fake.held) != 0 {
		t.Errorf("deletes = %d, held = %v", fake.deletes, fake.held)
	}
}

func TestServicePrincipalSchema(t *testing.T) {
	sch, _ := servicePrincipalSchema(t, &servicePrincipalResource{})
	for _, name := range []string{"name", "display_name", "description"} {
		attr, ok := sch.Attributes[name].(schema.StringAttribute)
		if !ok || len(attr.PlanModifiers) == 0 {
			t.Errorf("%s must replace the resource", name)
		}
	}
	if attr := sch.Attributes["rotation_trigger"].(schema.StringAttribute); len(attr.PlanModifiers) != 0 {
		t.Error("rotation_trigger rotates in place and must not replace")
	}
	if attr := sch.Attributes["client_secret"].(schema.StringAttribute); !attr.Sensitive || !attr.Computed || attr.Optional {
		t.Error("client_secret must be computed and sensitive")
	}

	name := func(in string) bool {
		var resp validator.StringResponse
		servicePrincipalNameValidator{}.ValidateString(context.Background(),
			validator.StringRequest{ConfigValue: types.StringValue(in)}, &resp)
		return !resp.Diagnostics.HasError()
	}
	if !name("ci") || !name("ci-pipeline-2") || name("Ci") || name("1ci") || name("ci_x") || name("") ||
		name("a"+strings.Repeat("b", 40)) || !name("a"+strings.Repeat("b", 39)) {
		t.Error("name must match ^[a-z][a-z0-9-]{0,39}$")
	}

	overlap := func(in int64) bool {
		var resp validator.Int64Response
		rotationOverlapValidator{}.ValidateInt64(context.Background(),
			validator.Int64Request{ConfigValue: types.Int64Value(in)}, &resp)
		return !resp.Diagnostics.HasError()
	}
	if !overlap(30) || !overlap(86400) || overlap(29) || overlap(86401) {
		t.Error("rotation_overlap_seconds must be 30..86400")
	}
}
