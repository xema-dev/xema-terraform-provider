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

// orgModelDefaultKind is the XemaResourceKind wire value for the organization
// default model: PUT/GET/DELETE /org-model-default upstream.
const orgModelDefaultKind = "org-model-default"

var (
	_ resource.Resource                = (*orgModelDefaultResource)(nil)
	_ resource.ResourceWithConfigure   = (*orgModelDefaultResource)(nil)
	_ resource.ResourceWithImportState = (*orgModelDefaultResource)(nil)
)

type orgModelDefaultResource struct {
	client *client.Client
}

// NewOrgModelDefaultResource is the factory registered with the provider.
func NewOrgModelDefaultResource() resource.Resource {
	return &orgModelDefaultResource{}
}

// orgModelDefaultModel mirrors the org-model-default kind spec. The default is
// one record per organization, so `id` is only the control-plane handle.
type orgModelDefaultModel struct {
	ID      types.String `tfsdk:"id"`
	ModelID types.String `tfsdk:"model_id"`
}

func (r *orgModelDefaultResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_model_default"
}

func (r *orgModelDefaultResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The organization default model: what an invocation routes to when no run override, " +
			"agent override or rule applies. One per organization; the resolver never substitutes a " +
			"platform default when it is unset. Managed through llm-registry-api via the control plane " +
			"(an org-admin write).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Control-plane physical id of the organization default.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"model_id": schema.StringAttribute{
				Required: true,
				Description: "Registry model record id (`LlmModel.id`). It must be served by an active " +
					"provider; otherwise the write is refused.",
			},
		},
	}
}

func (r *orgModelDefaultResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (m orgModelDefaultModel) toSpec() map[string]any {
	return map[string]any{"modelId": m.ModelID.ValueString()}
}

func (m *orgModelDefaultModel) applyReadback(spec map[string]any) {
	m.ModelID = types.StringValue(specString(spec, "modelId"))
}

func (r *orgModelDefaultResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgModelDefaultModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	handle, err := r.client.Create(ctx, orgModelDefaultKind, plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to set org model default", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, orgModelDefaultKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read org model default after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgModelDefaultResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgModelDefaultModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, orgModelDefaultKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read org model default", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgModelDefaultResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan orgModelDefaultModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Update(ctx, orgModelDefaultKind, plan.ID.ValueString(), plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to update org model default", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgModelDefaultResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgModelDefaultModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, orgModelDefaultKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to unset org model default", err.Error())
	}
}

func (r *orgModelDefaultResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
