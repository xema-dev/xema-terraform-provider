// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

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
type biomeInstallModel struct {
	ID           types.String         `tfsdk:"id"`
	BiomeID      types.String         `tfsdk:"biome_id"`
	ConfigJson   jsontypes.Normalized `tfsdk:"config_json"`
	Connections jsontypes.Normalized `tfsdk:"connections"`
	Resources    jsontypes.Normalized `tfsdk:"resources"`
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
				Description: "Biome id to install. Immutable natural key (one install per biomeId per org).",
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
		},
	}
}

func (r *biomeInstallResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
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

	integrations, err := normalizedToValue(m.Connections)
	if err != nil {
		return nil, err
	}
	if integrations != nil {
		spec["connections"] = integrations
	}

	resources, err := normalizedToValue(m.Resources)
	if err != nil {
		return nil, err
	}
	if resources != nil {
		spec["resources"] = resources
	}

	return spec, nil
}

func (m *biomeInstallModel) applyReadback(spec map[string]any) {
	m.BiomeID = types.StringValue(specString(spec, "biomeId"))
	m.ConfigJson = specNormalized(spec, "configJson")
	m.Connections = specNormalized(spec, "connections")
	m.Resources = specNormalized(spec, "resources")
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
		resp.Diagnostics.AddError("Failed to create biome install", err.Error())
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
		resp.Diagnostics.AddError("Failed to update biome install", err.Error())
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
