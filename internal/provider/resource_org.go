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

// orgKind is the XemaResourceKind wire value for organizations.
const orgKind = "org"

var (
	_ resource.Resource                = (*orgResource)(nil)
	_ resource.ResourceWithConfigure   = (*orgResource)(nil)
	_ resource.ResourceWithImportState = (*orgResource)(nil)
)

type orgResource struct {
	client *client.Client
}

// NewOrgResource is the factory registered with the provider.
func NewOrgResource() resource.Resource {
	return &orgResource{}
}

// orgModel mirrors the org kind spec. `name` is the immutable realm-unique slug.
type orgModel struct {
	ID          types.String         `tfsdk:"id"`
	Name        types.String         `tfsdk:"name"`
	DisplayName types.String         `tfsdk:"display_name"`
	Domain      types.String         `tfsdk:"domain"`
	Metadata    jsontypes.Normalized `tfsdk:"metadata"`
}

func (r *orgResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org"
}

func (r *orgResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Xema organization (tenant), managed through identity-api via the control plane. " +
			"Creating or deleting an org requires a platform-admin (operator) token; an org admin may only " +
			"read/update their own org.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Organization id (the control-plane physical id / identity org id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Organization slug (realm-unique, lowercase). Immutable natural key.",
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable organization name.",
			},
			"domain": schema.StringAttribute{
				Optional:    true,
				Description: "Optional org domain (unique within the realm).",
			},
			"metadata": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional free-form metadata as a JSON object.",
			},
		},
	}
}

func (r *orgResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

func (m orgModel) toSpec() (map[string]any, error) {
	spec := map[string]any{
		"name":        m.Name.ValueString(),
		"displayName": m.DisplayName.ValueString(),
	}
	if d := optString(m.Domain); d != "" {
		spec["domain"] = d
	}
	meta, err := normalizedToValue(m.Metadata)
	if err != nil {
		return nil, err
	}
	if meta != nil {
		spec["metadata"] = meta
	}
	return spec, nil
}

func (m *orgModel) applyReadback(spec map[string]any) {
	m.Name = types.StringValue(specString(spec, "name"))
	m.DisplayName = types.StringValue(specString(spec, "displayName"))
	m.Domain = strOrNull(specString(spec, "domain"))
	m.Metadata = specNormalized(spec, "metadata")
}

func (r *orgResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid metadata", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, orgKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create organization", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, orgKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read organization after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, orgKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read organization", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan orgModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid metadata", err.Error())
		return
	}
	res, err := r.client.Update(ctx, orgKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update organization", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, orgKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete organization", err.Error())
	}
}

func (r *orgResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
