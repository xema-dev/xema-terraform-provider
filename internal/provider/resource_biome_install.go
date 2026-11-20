// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// biomeInstallKind is the XemaResourceKind wire value for org-scoped biome
// installs.
const biomeInstallKind = "biome-install"

// The typed refusals of the biome-install kind that the provider explains.
const (
	// biomeInstallConsentRefusedCode: the install needs a grant consent the
	// control plane could not give; the error names the stop reason.
	biomeInstallConsentRefusedCode = "CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSED"
	// biomeInstallConsentRefusalUnmappedCode: the consent door refused with a
	// status and code control-plane maps to no reason (a defect to report).
	biomeInstallConsentRefusalUnmappedCode = "CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSAL_UNMAPPED"
	// biomeInstallSpecInvalidCode: the spec's `acceptPermissions` is not a boolean.
	biomeInstallSpecInvalidCode = "CONTROL_PLANE_BIOME_INSTALL_SPEC_INVALID"
)

var (
	_ resource.Resource                = (*biomeInstallResource)(nil)
	_ resource.ResourceWithConfigure   = (*biomeInstallResource)(nil)
	_ resource.ResourceWithImportState = (*biomeInstallResource)(nil)
)

type biomeInstallResource struct {
	client *client.Client
}

// NewBiomeInstallResource is the factory registered with the provider.
func NewBiomeInstallResource() resource.Resource {
	return &biomeInstallResource{}
}

// biomeInstallModel mirrors the biome-install kind spec. Installs are
// org-scoped; `biome_id` is the immutable natural key (one install per biomeId
// per org). The pinned version is service-managed and not represented here.
//
// AcceptPermissions is the operator's intent, sent to the control plane and
// never read back: the server's view has no such field, so state keeps the
// configured value and it never reads as drift.
type biomeInstallModel struct {
	ID                types.String         `tfsdk:"id"`
	BiomeID           types.String         `tfsdk:"biome_id"`
	ConfigJson        jsontypes.Normalized `tfsdk:"config_json"`
	Connections       jsontypes.Normalized `tfsdk:"connections"`
	Resources         jsontypes.Normalized `tfsdk:"resources"`
	AcceptPermissions types.Bool           `tfsdk:"accept_permissions"`
}

func (r *biomeInstallResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_biome_install"
}

func (r *biomeInstallResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An org-scoped biome install, managed through the control-plane biome-install adapter. " +
			"There is one install per biomeId per org; the pinned biome version is immutable and " +
			"service-managed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Biome install id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"biome_id": schema.StringAttribute{
				Required:    true,
				Description: "Biome id to install. Immutable natural key (one install per biomeId per org); changing it replaces the install.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"config_json": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Install configuration as a JSON object.",
			},
			"connections": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Bound org connections as a JSON array of { adapterKind, orgConnectionId }.",
			},
			"resources": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Bound resources as a JSON array of { adapterKind, selector }.",
			},
			"accept_permissions": schema.BoolAttribute{
				Optional: true,
				Description: "Consent to the grant set the install plan asks for. The control plane plans the install, " +
					"gives the plan's grant consent through the credential's own consent door when this is true, and " +
					"runs the install bound to that plan; unset means false, and an install that needs consent is " +
					"then refused before it runs. A grant set that changed since it was planned is never approved. " +
					"It acts only when the install is created: changing it on an existing install updates nothing " +
					"(it never replaces the install). This is intent only: it is not read back.",
			},
		},
	}
}

func (r *biomeInstallResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (m biomeInstallModel) toSpec() (map[string]any, error) {
	spec := map[string]any{
		"biomeId": m.BiomeID.ValueString(),
	}

	configJson, err := normalizedToValue(m.ConfigJson)
	if err != nil {
		return nil, err
	}
	if configJson != nil {
		spec["configJson"] = configJson
	}

	connections, err := normalizedToValue(m.Connections)
	if err != nil {
		return nil, err
	}
	if connections != nil {
		spec["connections"] = connections
	}

	resources, err := normalizedToValue(m.Resources)
	if err != nil {
		return nil, err
	}
	if resources != nil {
		spec["resources"] = resources
	}

	// Sent only when set: absent means false to the control plane.
	if !m.AcceptPermissions.IsNull() && !m.AcceptPermissions.IsUnknown() {
		spec["acceptPermissions"] = m.AcceptPermissions.ValueBool()
	}

	return spec, nil
}

// applyReadback sets the fields the server reports. AcceptPermissions is not
// one of them, so the value in the model (the configured intent) is kept.
func (m *biomeInstallModel) applyReadback(spec map[string]any) {
	m.BiomeID, m.ConfigJson, m.Connections, m.Resources = biomeInstallReadback(spec)
}

// biomeInstallReadback reads the server-reported fields of a biome install.
func biomeInstallReadback(spec map[string]any) (types.String, jsontypes.Normalized, jsontypes.Normalized, jsontypes.Normalized) {
	return types.StringValue(specString(spec, "biomeId")),
		specNormalized(spec, "configJson"),
		specNormalized(spec, "connections"),
		specNormalized(spec, "resources")
}

// biomeInstallFailure explains a failed create or update. The typed consent
// and spec refusals name what to change; anything else keeps the generic
// summary. Only the error's code and stop reason are quoted, never the request.
func biomeInstallFailure(summary string, err error, accept types.Bool) (string, string) {
	if apiErr := client.AsAPIError(err, biomeInstallConsentRefusedCode); apiErr != nil && apiErr.Status == http.StatusUnprocessableEntity {
		return biomeInstallConsentRefused(apiErr, accept)
	}
	if apiErr := client.AsAPIError(err, biomeInstallConsentRefusalUnmappedCode); apiErr != nil && apiErr.Status == http.StatusBadGateway {
		return biomeInstallConsentRefusalUnmapped(apiErr)
	}
	if apiErr := client.AsAPIError(err, biomeInstallSpecInvalidCode); apiErr != nil && apiErr.Status == http.StatusBadRequest {
		return "Biome install spec refused", fmt.Sprintf("The control plane refused the spec (%s): "+
			"`accept_permissions` must be a boolean; unset means false. The provider and the control plane may be "+
			"on incompatible versions.", apiErr.Code)
	}
	return summary, err.Error()
}

func (r *biomeInstallResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan biomeInstallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid biome install spec", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, biomeInstallKind, spec)
	if err != nil {
		resp.Diagnostics.AddError(biomeInstallFailure("Failed to create biome install", err, plan.AcceptPermissions))
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, biomeInstallKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read biome install after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *biomeInstallResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state biomeInstallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, biomeInstallKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read biome install", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *biomeInstallResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan biomeInstallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid biome install spec", err.Error())
		return
	}
	res, err := r.client.Update(ctx, biomeInstallKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError(biomeInstallFailure("Failed to update biome install", err, plan.AcceptPermissions))
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *biomeInstallResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state biomeInstallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, biomeInstallKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete biome install", err.Error())
	}
}

func (r *biomeInstallResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
