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

// spaceKind is the XemaResourceKind wire value for spaces.
const spaceKind = "space"

var (
	_ resource.Resource                = (*spaceResource)(nil)
	_ resource.ResourceWithConfigure   = (*spaceResource)(nil)
	_ resource.ResourceWithImportState = (*spaceResource)(nil)
)

type spaceResource struct {
	client *client.Client
}

// NewSpaceResource is the factory registered with the provider.
func NewSpaceResource() resource.Resource {
	return &spaceResource{}
}

// spaceModel mirrors the space kind spec. Only `classification` is updatable in
// place; `ref`, `display_name`, and `labels` are create-only and force replace.
type spaceModel struct {
	ID             types.String         `tfsdk:"id"`
	Ref            types.String         `tfsdk:"ref"`
	DisplayName    types.String         `tfsdk:"display_name"`
	Classification types.String         `tfsdk:"classification"`
	Labels         jsontypes.Normalized `tfsdk:"labels"`
}

func (r *spaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_space"
}

func (r *spaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Xema Space, managed through space-registry-api via the control plane. " +
			"Only `classification` is updatable in place; changing `ref`, `display_name`, or `labels` forces replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted space id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": schema.StringAttribute{
				Required:    true,
				Description: "Canonical Space ref (natural key). Create-only; changing it forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable space name. Create-only; changing it forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"classification": schema.StringAttribute{
				Optional:    true,
				Description: "Optional space classification. The only field updatable in place.",
			},
			"labels": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional free-form labels as a JSON object. Create-only; changing it forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *spaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

func (m spaceModel) toSpec() (map[string]any, error) {
	spec := map[string]any{
		"ref":         m.Ref.ValueString(),
		"displayName": m.DisplayName.ValueString(),
	}
	if c := optString(m.Classification); c != "" {
		spec["classification"] = c
	}
	labels, err := normalizedToValue(m.Labels)
	if err != nil {
		return nil, err
	}
	if labels != nil {
		spec["labels"] = labels
	}
	return spec, nil
}

func (m *spaceModel) applyReadback(spec map[string]any) {
	m.Ref = types.StringValue(specString(spec, "ref"))
	m.DisplayName = types.StringValue(specString(spec, "displayName"))
	m.Classification = strOrNull(specString(spec, "classification"))
	m.Labels = specNormalized(spec, "labels")
}

func (r *spaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan spaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid labels", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, spaceKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create space", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, spaceKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read space after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *spaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state spaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, spaceKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read space", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *spaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan spaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid labels", err.Error())
		return
	}
	res, err := r.client.Update(ctx, spaceKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update space", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *spaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state spaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, spaceKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete space", err.Error())
	}
}

func (r *spaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
