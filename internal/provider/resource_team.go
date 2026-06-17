// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// teamKind is the XemaResourceKind wire value for teams.
const teamKind = "team"

var (
	_ resource.Resource                = (*teamResource)(nil)
	_ resource.ResourceWithConfigure   = (*teamResource)(nil)
	_ resource.ResourceWithImportState = (*teamResource)(nil)
)

type teamResource struct {
	client *client.Client
}

// NewTeamResource is the factory registered with the provider.
func NewTeamResource() resource.Resource {
	return &teamResource{}
}

// teamModel mirrors the team kind spec (slug, displayName, description) plus the
// server-owned physical id stored in state. The slug is the immutable natural
// key; built-in teams are not manageable here.
type teamModel struct {
	ID          types.String `tfsdk:"id"`
	Slug        types.String `tfsdk:"slug"`
	DisplayName types.String `tfsdk:"display_name"`
	Description types.String `tfsdk:"description"`
}

func (r *teamResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}

func (r *teamResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Xema team, managed through authorization-api via the control plane.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted team id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "Team slug (org-unique, kebab-case). Immutable natural key.",
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable team name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Free-form team description.",
			},
		},
	}
}

func (r *teamResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

func (m teamModel) toSpec() map[string]any {
	spec := map[string]any{
		"slug":        m.Slug.ValueString(),
		"displayName": m.DisplayName.ValueString(),
	}
	if d := optString(m.Description); d != "" {
		spec["description"] = d
	}
	return spec
}

func (m *teamModel) applyReadback(spec map[string]any) {
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.DisplayName = types.StringValue(specString(spec, "displayName"))
	m.Description = strOrNull(specString(spec, "description"))
}

func (r *teamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan teamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	handle, err := r.client.Create(ctx, teamKind, plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create team", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *teamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state teamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, teamKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read team", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *teamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan teamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Update(ctx, teamKind, plan.ID.ValueString(), plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to update team", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *teamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state teamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, teamKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete team", err.Error())
	}
}

func (r *teamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
