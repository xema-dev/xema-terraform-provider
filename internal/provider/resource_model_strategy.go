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

// modelStrategyKind is the XemaResourceKind wire value for model strategies.
const modelStrategyKind = "model-strategy"

var (
	_ resource.Resource                = (*modelStrategyResource)(nil)
	_ resource.ResourceWithConfigure   = (*modelStrategyResource)(nil)
	_ resource.ResourceWithImportState = (*modelStrategyResource)(nil)
)

type modelStrategyResource struct {
	client *client.Client
}

// NewModelStrategyResource is the factory registered with the provider.
func NewModelStrategyResource() resource.Resource {
	return &modelStrategyResource{}
}

type modelStrategyModel struct {
	ID          types.String `tfsdk:"id"`
	Slug        types.String `tfsdk:"slug"`
	DisplayName types.String `tfsdk:"display_name"`
	Description types.String `tfsdk:"description"`
}

func (r *modelStrategyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model_strategy"
}

func (r *modelStrategyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// `description` is Optional AND Computed: the XEMA API is authoritative for
	// current state, and a declaration that omits it must be able to read back
	// the service's value (null or set) without Terraform refusing its own
	// result with "Provider produced inconsistent result after apply".
	resp.Schema = schema.Schema{
		Description: "A model strategy: a named container of model resolution rules, bound to projects. " +
			"It holds only rules — declare them with `xema_model_resolution_rule` and its `strategy_id`. " +
			"Managed through llm-registry-api via the control plane. The API is authoritative: every " +
			"server-owned value is refreshed from it on each read, so state can be rebuilt by import.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted strategy id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"slug": schema.StringAttribute{
				Required: true,
				Description: "Stable slug, unique within the organization. It is the managed key, " +
					"and it is immutable upstream — changing it declares a different strategy.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable strategy name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "What this strategy is for.",
			},
		},
	}
}

func (r *modelStrategyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (m modelStrategyModel) toSpec() map[string]any {
	spec := map[string]any{
		"slug":        m.Slug.ValueString(),
		"displayName": m.DisplayName.ValueString(),
	}
	if s := optString(m.Description); s != "" {
		spec["description"] = s
	}
	return spec
}

func (m *modelStrategyModel) applyReadback(spec map[string]any) {
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.DisplayName = types.StringValue(specString(spec, "displayName"))
	m.Description = strOrNull(specString(spec, "description"))
}

func (r *modelStrategyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modelStrategyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	handle, err := r.client.Create(ctx, modelStrategyKind, plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create model strategy", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, modelStrategyKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read model strategy after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelStrategyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state modelStrategyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, modelStrategyKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read model strategy", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *modelStrategyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan modelStrategyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Update(ctx, modelStrategyKind, plan.ID.ValueString(), plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to update model strategy", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelStrategyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state modelStrategyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, modelStrategyKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete model strategy", err.Error())
	}
}

func (r *modelStrategyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
