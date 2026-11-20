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

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

type biomeInstallServer struct {
	*httptest.Server
	// sent is the spec of the last create or update.
	sent map[string]any
	// refusal, when set, is the status and body every create and update answers.
	refusalStatus int
	refusal       map[string]any
}

// fakeBiomeInstallControlPlane answers as the biome-install kind does: the
// read-back carries the install's biomeId, configJson, connections and
// resources, and never acceptPermissions.
func fakeBiomeInstallControlPlane(t *testing.T) *biomeInstallServer {
	t.Helper()
	const base = "/control-plane/resources/biome-install"
	fake := &biomeInstallServer{}
	readback := func() map[string]any {
		spec := map[string]any{}
		for _, k := range []string{"biomeId", "configJson", "connections", "resources"} {
			spec[k] = fake.sent[k]
		}
		return map[string]any{"kind": "biome-install", "physicalId": "inst-1", "managedKey": spec["biomeId"], "spec": spec}
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
			if fake.refusal != nil {
				w.WriteHeader(fake.refusalStatus)
				_ = json.NewEncoder(w).Encode(fake.refusal)
				return
			}
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			w.WriteHeader(http.StatusCreated)
			writeData(w, map[string]any{"kind": "biome-install", "physicalId": "inst-1", "managedKey": fake.sent["biomeId"]})
		case (r.Method == http.MethodGet || r.Method == http.MethodPut) && r.URL.Path == base+"/inst-1" && fake.sent != nil:
			writeData(w, readback())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func biomeInstallSchema(t *testing.T, r *biomeInstallResource) (schema.Schema, tftypes.Object) {
	t.Helper()
	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	return sr.Schema, sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
}

// biomeInstallPlan is a planned install; accept is the configured
// accept_permissions, nil for unset.
func biomeInstallPlan(obj tftypes.Object, id any, accept *bool) tftypes.Value {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	acceptValue := tftypes.NewValue(tftypes.Bool, nil)
	if accept != nil {
		acceptValue = tftypes.NewValue(tftypes.Bool, *accept)
	}
	return tftypes.NewValue(obj, map[string]tftypes.Value{
		"id":                 tftypes.NewValue(tftypes.String, id),
		"biome_id":           str("slack-connector"),
		"config_json":        str(`{"defaultChannel":"#general"}`),
		"connections":        str(`[]`),
		"resources":          str(`[]`),
		"accept_permissions": acceptValue,
	})
}

func createBiomeInstall(t *testing.T, fake *biomeInstallServer, accept *bool) (*biomeInstallResource, resource.CreateResponse) {
	t.Helper()
	r := &biomeInstallResource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	sch, obj := biomeInstallSchema(t, r)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(obj, nil)}}
	r.Create(context.Background(), resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: sch, Raw: biomeInstallPlan(obj, tftypes.UnknownValue, accept)},
	}, &resp)
	return r, resp
}

func stateAccept(t *testing.T, state tfsdk.State) biomeInstallModel {
	t.Helper()
	var got biomeInstallModel
	if diags := state.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	return got
}

func onlyError(t *testing.T, diags diag.Diagnostics) diag.Diagnostic {
	t.Helper()
	errs := diags.Errors()
	if len(errs) != 1 {
		t.Fatalf("want one error, got %v", diags)
	}
	return errs[0]
}

func TestBiomeInstallSendsAcceptPermissionsAndKeepsIt(t *testing.T) {
	fake := fakeBiomeInstallControlPlane(t)
	accept := true
	r, resp := createBiomeInstall(t, fake, &accept)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	if fake.sent["acceptPermissions"] != true {
		t.Errorf("spec.acceptPermissions = %v, want true", fake.sent["acceptPermissions"])
	}
	if got := stateAccept(t, resp.State); !got.AcceptPermissions.ValueBool() || got.ID.ValueString() != "inst-1" {
		t.Errorf("the read-back lacks acceptPermissions, and state must keep the configured value: %#v", got)
	}

	// A refresh reads the server, which never reports the field: no drift.
	readResp := resource.ReadResponse{State: resp.State}
	r.Read(context.Background(), resource.ReadRequest{State: resp.State}, &readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("read: %v", readResp.Diagnostics)
	}
	if got := stateAccept(t, readResp.State); got.AcceptPermissions.IsNull() || !got.AcceptPermissions.ValueBool() {
		t.Errorf("refresh dropped accept_permissions: %#v", got)
	}
}

func TestBiomeInstallUnsetAcceptPermissionsIsNotSent(t *testing.T) {
	fake := fakeBiomeInstallControlPlane(t)
	_, resp := createBiomeInstall(t, fake, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	if _, sent := fake.sent["acceptPermissions"]; sent {
		t.Errorf("an unset accept_permissions was sent: %#v", fake.sent)
	}
	if got := stateAccept(t, resp.State); !got.AcceptPermissions.IsNull() {
		t.Errorf("accept_permissions = %v, want null", got.AcceptPermissions)
	}
}

func TestBiomeInstallUpdateSendsAndKeepsAcceptPermissions(t *testing.T) {
	fake := fakeBiomeInstallControlPlane(t)
	r, createResp := createBiomeInstall(t, fake, nil)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("create: %v", createResp.Diagnostics)
	}
	sch, obj := biomeInstallSchema(t, r)
	accept := false
	resp := resource.UpdateResponse{State: createResp.State}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: sch, Raw: biomeInstallPlan(obj, "inst-1", &accept)},
		State: createResp.State,
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	if fake.sent["acceptPermissions"] != false {
		t.Errorf("spec.acceptPermissions = %v, want false", fake.sent["acceptPermissions"])
	}
	if got := stateAccept(t, resp.State); got.AcceptPermissions.IsNull() || got.AcceptPermissions.ValueBool() {
		t.Errorf("accept_permissions = %v, want false", got.AcceptPermissions)
	}
}

func TestBiomeInstallAcceptPermissionsNeverReplaces(t *testing.T) {
	sch, _ := biomeInstallSchema(t, &biomeInstallResource{})
	attr, ok := sch.Attributes["accept_permissions"].(schema.BoolAttribute)
	if !ok {
		t.Fatal("accept_permissions is not a bool attribute")
	}
	if !attr.Optional || attr.Computed || attr.Required {
		t.Errorf("accept_permissions must be optional only: %#v", attr)
	}
	if len(attr.PlanModifiers) != 0 {
		t.Error("changing accept_permissions must update in place, never replace")
	}
}

func TestBiomeInstallConsentRefusedNamesTheRemedy(t *testing.T) {
	refusal := map[string]any{
		"code":    "CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSED",
		"reason":  "row-still-held",
		"message": "The lifecycle install of 'slack-connector' needs a grant consent that was not given (row-still-held); nothing ran.",
	}
	for _, tc := range []struct {
		name       string
		accept     *bool
		wantRemedy bool
	}{
		{name: "unset", accept: nil, wantRemedy: true},
		{name: "false", accept: func() *bool { b := false; return &b }(), wantRemedy: true},
		{name: "true", accept: func() *bool { b := true; return &b }(), wantRemedy: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := fakeBiomeInstallControlPlane(t)
			fake.refusalStatus, fake.refusal = http.StatusUnprocessableEntity, refusal
			_, resp := createBiomeInstall(t, fake, tc.accept)
			d := onlyError(t, resp.Diagnostics)
			if d.Summary() != "Biome install consent refused" {
				t.Errorf("summary = %q", d.Summary())
			}
			if !strings.Contains(d.Detail(), "stop reason: row-still-held") {
				t.Errorf("detail names no stop reason: %q", d.Detail())
			}
			if got := strings.Contains(d.Detail(), "Set `accept_permissions = true`"); got != tc.wantRemedy {
				t.Errorf("remedy shown = %v, want %v: %q", got, tc.wantRemedy, d.Detail())
			}
			if strings.Contains(d.Detail(), "tok") {
				t.Error("the detail quotes the credential")
			}
		})
	}
}

func TestBiomeInstallConsentRefusedAfterTheRun(t *testing.T) {
	fake := fakeBiomeInstallControlPlane(t)
	fake.refusalStatus = http.StatusUnprocessableEntity
	fake.refusal = map[string]any{"code": "CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSED", "reason": "after-run-consent-not-given", "message": "after the run"}
	_, resp := createBiomeInstall(t, fake, nil)
	d := onlyError(t, resp.Diagnostics)
	if d.Summary() != "Biome install consent refused after the install ran" {
		t.Errorf("summary = %q", d.Summary())
	}
	if strings.Contains(d.Detail(), "accept_permissions") || strings.Contains(d.Detail(), "nothing ran") {
		t.Errorf("an install that ran is neither fixed by accept_permissions nor unrun: %q", d.Detail())
	}
}

func TestBiomeInstallConsentRefusedWithoutReasonIsGeneric(t *testing.T) {
	fake := fakeBiomeInstallControlPlane(t)
	fake.refusalStatus = http.StatusUnprocessableEntity
	fake.refusal = map[string]any{"code": "CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSED", "message": "refused"}
	_, resp := createBiomeInstall(t, fake, nil)
	d := onlyError(t, resp.Diagnostics)
	if d.Summary() != "Biome install consent refused" {
		t.Errorf("an empty reason is not an install that ran: summary = %q", d.Summary())
	}
	if !strings.Contains(d.Detail(), "stop reason: not reported") || !strings.Contains(d.Detail(), "Set `accept_permissions = true`") {
		t.Errorf("detail = %q", d.Detail())
	}
}

func TestBiomeInstallConsentRefusedHintsPerReason(t *testing.T) {
	accepted := func() *bool { b := true; return &b }()
	for _, tc := range []struct {
		reason string
		accept *bool
		want   string
	}{
		{reason: "not-authorized", accept: accepted, want: "`authorization:installation-consent.decide@1`"},
		{reason: "not-authenticated", accept: accepted, want: "XEMA_TOKEN"},
		{reason: "unavailable", accept: accepted, want: "apply again later"},
		{reason: "request-not-found", accept: accepted, want: "plan the install afresh"},
		{reason: "conferral-revision-moved", accept: accepted, want: "plan the install afresh"},
		{reason: "digest-changed", accept: accepted, want: "plan the install afresh"},
		{reason: "request-settled", accept: accepted, want: "plan the install afresh"},
		{reason: "superseded", accept: accepted, want: "plan the install afresh"},
		{reason: "admission-moved", accept: accepted, want: "plan the install afresh"},
		{reason: "eligibility-expired", accept: accepted, want: "plan the install afresh"},
		{reason: "not-authenticated", accept: nil, want: "XEMA_TOKEN"},
		{reason: "authority-unrepresentable", accept: accepted, want: ""},
		{reason: "store-ceiling-refused", accept: accepted, want: ""},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			fake := fakeBiomeInstallControlPlane(t)
			fake.refusalStatus = http.StatusUnprocessableEntity
			fake.refusal = map[string]any{"code": "CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSED", "reason": tc.reason, "message": "refused"}
			_, resp := createBiomeInstall(t, fake, tc.accept)
			d := onlyError(t, resp.Diagnostics)
			if d.Summary() != "Biome install consent refused" {
				t.Errorf("summary = %q", d.Summary())
			}
			if !strings.Contains(d.Detail(), "stop reason: "+tc.reason+")") {
				t.Errorf("detail does not quote the reason: %q", d.Detail())
			}
			if tc.want != "" && !strings.Contains(d.Detail(), tc.want) {
				t.Errorf("detail lacks %q: %q", tc.want, d.Detail())
			}
			if role := strings.Contains(d.Detail(), "installation-consent.decide"); role != (tc.reason == "not-authorized") {
				t.Errorf("role hint shown = %v for %s: %q", role, tc.reason, d.Detail())
			}
			if strings.Contains(d.Detail(), "accept_permissions") {
				t.Errorf("a reason with its own remedy, or an accepted plan, does not ask for accept_permissions: %q", d.Detail())
			}
		})
	}
}

func TestBiomeInstallConsentRefusalUnmappedSaysReportIt(t *testing.T) {
	fake := fakeBiomeInstallControlPlane(t)
	fake.refusalStatus = http.StatusBadGateway
	fake.refusal = map[string]any{
		"code":    "CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSAL_UNMAPPED",
		"message": "The consent door refused the grant consent of 'slack-connector' with HTTP 418 TEAPOT, which this service maps to no consent reason (a defect to report); nothing ran.",
	}
	_, resp := createBiomeInstall(t, fake, nil)
	d := onlyError(t, resp.Diagnostics)
	if d.Summary() != "Biome install consent refusal unmapped" {
		t.Errorf("summary = %q", d.Summary())
	}
	for _, want := range []string{"unmapped consent refusal", "HTTP 418 TEAPOT", "CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSAL_UNMAPPED", "Report it"} {
		if !strings.Contains(d.Detail(), want) {
			t.Errorf("detail lacks %q: %q", want, d.Detail())
		}
	}
}

func TestBiomeInstallSpecInvalidIsExplained(t *testing.T) {
	fake := fakeBiomeInstallControlPlane(t)
	fake.refusalStatus = http.StatusBadRequest
	fake.refusal = map[string]any{"code": "CONTROL_PLANE_BIOME_INSTALL_SPEC_INVALID", "message": "`acceptPermissions` is a boolean; absent means false."}
	_, resp := createBiomeInstall(t, fake, nil)
	d := onlyError(t, resp.Diagnostics)
	if d.Summary() != "Biome install spec refused" || !strings.Contains(d.Detail(), "CONTROL_PLANE_BIOME_INSTALL_SPEC_INVALID") {
		t.Errorf("diagnostic = %q: %q", d.Summary(), d.Detail())
	}
}

func TestBiomeInstallOtherFailuresKeepTheGenericSummary(t *testing.T) {
	fake := fakeBiomeInstallControlPlane(t)
	fake.refusalStatus = http.StatusUnprocessableEntity
	fake.refusal = map[string]any{"code": "CONTROL_PLANE_BIOME_INSTALL_REFUSED", "message": "refused"}
	_, resp := createBiomeInstall(t, fake, nil)
	if d := onlyError(t, resp.Diagnostics); d.Summary() != "Failed to create biome install" {
		t.Errorf("summary = %q", d.Summary())
	}
}

func TestBiomeInstallDataSourceHasNoIntent(t *testing.T) {
	var resp datasource.SchemaResponse
	NewBiomeInstallDataSource().Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["accept_permissions"]; ok {
		t.Error("accept_permissions is intent, never read back, so the data source cannot report it")
	}
}

func TestBiomeInstallDataSourceReadsTheServer(t *testing.T) {
	fake := fakeBiomeInstallControlPlane(t)
	fake.sent = map[string]any{"biomeId": "slack-connector", "configJson": map[string]any{}, "connections": []any{}, "resources": []any{}}
	ctx := context.Background()
	d := &biomeInstallDataSource{client: client.New(fake.URL, "org-1", "tok", fake.Client())}
	var sr datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sr)
	obj := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	null := tftypes.NewValue(tftypes.String, nil)
	config := tftypes.NewValue(obj, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, "inst-1"), "biome_id": null, "config_json": null, "connections": null, "resources": null,
	})
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(obj, nil)}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: config}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("read: %v", resp.Diagnostics)
	}
	var got biomeInstallDataModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if got.BiomeID.ValueString() != "slack-connector" || got.ConfigJson.ValueString() != "{}" {
		t.Errorf("unexpected state: %#v", got)
	}
}
