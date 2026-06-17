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

// environmentKind is the XemaResourceKind wire value for execution environments.
const environmentKind = "environment"

var (
	_ resource.Resource                = (*environmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*environmentResource)(nil)
	_ resource.ResourceWithImportState = (*environmentResource)(nil)
)

type environmentResource struct {
	client *client.Client
}

// NewEnvironmentResource is the factory registered with the provider.
func NewEnvironmentResource() resource.Resource {
	return &environmentResource{}
}

// environmentModel mirrors the environment kind spec. `slug` is the immutable
// natural key and the control-plane physical id.
type environmentModel struct {
	ID                  types.String         `tfsdk:"id"`
	Slug                types.String         `tfsdk:"slug"`
	Kind                types.String         `tfsdk:"kind"`
	Description         types.String         `tfsdk:"description"`
	AllowedCapabilities types.List           `tfsdk:"allowed_capabilities"`
	ApprovalRules       jsontypes.Normalized `tfsdk:"approval_rules"`
	RuntimeLimits       jsontypes.Normalized `tfsdk:"runtime_limits"`
}

func (r *environmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (r *environmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Xema execution environment, managed through authorization-api via the control plane.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Environment id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "Environment slug (the control-plane physical id). Immutable natural key.",
			},
			"kind": schema.StringAttribute{
				Required:    true,
				Description: "Execution environment kind.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Free-form environment description.",
			},
			"allowed_capabilities": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Capability refs allowed within this environment.",
			},
			"approval_rules": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional approval rules as a JSON object or array.",
			},
			"runtime_limits": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional runtime limits as a JSON object.",
			},
		},
	}
}

func (r *environmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

func (m environmentModel) toSpec(ctx context.Context) (map[string]any, error) {
	spec := map[string]any{
		"slug": m.Slug.ValueString(),
		"kind": m.Kind.ValueString(),
	}
	if v := optString(m.Description); v != "" {
		spec["description"] = v
	}

	if !m.AllowedCapabilities.IsNull() && !m.AllowedCapabilities.IsUnknown() {
		var caps []string
		if diags := m.AllowedCapabilities.ElementsAs(ctx, &caps, false); diags.HasError() {
			return nil, errFromDiags(diags)
		}
		if len(caps) > 0 {
			spec["allowedCapabilities"] = caps
		}
	}

	approvalRules, err := normalizedToValue(m.ApprovalRules)
	if err != nil {
		return nil, err
	}
	if approvalRules != nil {
		spec["approvalRules"] = approvalRules
	}

	runtimeLimits, err := normalizedToValue(m.RuntimeLimits)
	if err != nil {
		return nil, err
	}
	if runtimeLimits != nil {
		spec["runtimeLimits"] = runtimeLimits
	}

	return spec, nil
}

func (m *environmentModel) applyReadback(ctx context.Context, spec map[string]any) error {
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.Kind = types.StringValue(specString(spec, "kind"))
	m.Description = strOrNull(specString(spec, "description"))

	if caps := stringList(spec, "allowedCapabilities"); len(caps) > 0 {
		lv, diags := types.ListValueFrom(ctx, types.StringType, caps)
		if diags.HasError() {
			return errFromDiags(diags)
		}
		m.AllowedCapabilities = lv
	} else {
		m.AllowedCapabilities = types.ListNull(types.StringType)
	}

	m.ApprovalRules = specNormalized(spec, "approvalRules")
	m.RuntimeLimits = specNormalized(spec, "runtimeLimits")
	return nil
}

func (r *environmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan environmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid environment", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, environmentKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create environment", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, environmentKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read environment after create", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map environment", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *environmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state environmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, environmentKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read environment", err.Error())
		return
	}
	if err := state.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map environment", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *environmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan environmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid environment", err.Error())
		return
	}
	res, err := r.client.Update(ctx, environmentKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update environment", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map environment", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *environmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state environmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, environmentKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete environment", err.Error())
	}
}

func (r *environmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
