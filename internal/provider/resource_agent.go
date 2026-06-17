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

// agentKind is the XemaResourceKind wire value for agents (agent compositions).
const agentKind = "agent"

var (
	_ resource.Resource                = (*agentResource)(nil)
	_ resource.ResourceWithConfigure   = (*agentResource)(nil)
	_ resource.ResourceWithImportState = (*agentResource)(nil)
)

type agentResource struct {
	client *client.Client
}

// NewAgentResource is the factory registered with the provider.
func NewAgentResource() resource.Resource {
	return &agentResource{}
}

// agentModel mirrors the agent kind spec. The (slug, version) pair is the
// immutable natural key; `root` is the root CompositionNode.
type agentModel struct {
	ID          types.String         `tfsdk:"id"`
	Slug        types.String         `tfsdk:"slug"`
	Version     types.String         `tfsdk:"version"`
	DisplayName types.String         `tfsdk:"display_name"`
	Scope       types.String         `tfsdk:"scope"`
	Root        jsontypes.Normalized `tfsdk:"root"`
	Description types.String         `tfsdk:"description"`
	Capability  types.String         `tfsdk:"capability"`
	Workspace   jsontypes.Normalized `tfsdk:"workspace"`
}

func (r *agentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent"
}

func (r *agentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Xema Agent (agent composition), managed through llm-registry-api via the control plane.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted agent id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "Agent slug. Immutable natural key.",
			},
			"version": schema.StringAttribute{
				Required:    true,
				Description: "Agent semver. Immutable natural key.",
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable agent name.",
			},
			"scope": schema.StringAttribute{
				Required:    true,
				Description: "CompositionScope value (User | Project | Org | Biome | System).",
			},
			"root": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "The root CompositionNode as a JSON object.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Free-form agent description.",
			},
			"capability": schema.StringAttribute{
				Optional:    true,
				Description: "Optional capability ref exposed by this agent.",
			},
			"workspace": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional workspace configuration as a JSON object.",
			},
		},
	}
}

func (r *agentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

func (m agentModel) toSpec() (map[string]any, error) {
	spec := map[string]any{
		"slug":        m.Slug.ValueString(),
		"version":     m.Version.ValueString(),
		"displayName": m.DisplayName.ValueString(),
		"scope":       m.Scope.ValueString(),
	}
	if v := optString(m.Description); v != "" {
		spec["description"] = v
	}
	if v := optString(m.Capability); v != "" {
		spec["capability"] = v
	}

	root, err := normalizedToValue(m.Root)
	if err != nil {
		return nil, err
	}
	spec["root"] = root

	workspace, err := normalizedToValue(m.Workspace)
	if err != nil {
		return nil, err
	}
	if workspace != nil {
		spec["workspace"] = workspace
	}
	return spec, nil
}

func (m *agentModel) applyReadback(spec map[string]any) {
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.Version = types.StringValue(specString(spec, "version"))
	m.DisplayName = types.StringValue(specString(spec, "displayName"))
	m.Scope = types.StringValue(specString(spec, "scope"))
	m.Description = strOrNull(specString(spec, "description"))
	m.Capability = strOrNull(specString(spec, "capability"))
	m.Root = specNormalized(spec, "root")
	m.Workspace = specNormalized(spec, "workspace")
}

func (r *agentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan agentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid agent", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, agentKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create agent", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, agentKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read agent after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *agentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state agentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, agentKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read agent", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *agentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan agentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid agent", err.Error())
		return
	}
	res, err := r.client.Update(ctx, agentKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update agent", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *agentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state agentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, agentKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete agent", err.Error())
	}
}

func (r *agentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
