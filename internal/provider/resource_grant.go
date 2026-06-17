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

// grantKind is the XemaResourceKind wire value for capability grants.
const grantKind = "grant"

var (
	_ resource.Resource                = (*grantResource)(nil)
	_ resource.ResourceWithConfigure   = (*grantResource)(nil)
	_ resource.ResourceWithImportState = (*grantResource)(nil)
)

type grantResource struct {
	client *client.Client
}

// NewGrantResource is the factory registered with the provider.
func NewGrantResource() resource.Resource {
	return &grantResource{}
}

// grantModel mirrors the grant kind spec. A grant binds a subject to a
// capability; its identity is the (subject, capability, resource, environment)
// tuple, so there is no author slug.
type grantModel struct {
	ID               types.String         `tfsdk:"id"`
	SubjectKind      types.String         `tfsdk:"subject_kind"`
	SubjectRef       types.String         `tfsdk:"subject_ref"`
	Capability       types.String         `tfsdk:"capability"`
	Environment      types.String         `tfsdk:"environment"`
	ResourceGlob     types.String         `tfsdk:"resource_glob"`
	RequiresApproval types.Bool           `tfsdk:"requires_approval"`
	Profile          types.String         `tfsdk:"profile"`
	Constraints      jsontypes.Normalized `tfsdk:"constraints"`
}

func (r *grantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_grant"
}

func (r *grantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A capability grant, managed through authorization-api via the control plane. " +
			"A grant has no author slug; its identity is the (subject, capability, resource, environment) tuple.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Grant id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"subject_kind": schema.StringAttribute{
				Required:    true,
				Description: "Kind of the subject the grant binds (e.g. user, service, agent).",
			},
			"subject_ref": schema.StringAttribute{
				Required:    true,
				Description: "Reference (slug/id) of the subject the grant binds.",
			},
			"capability": schema.StringAttribute{
				Required:    true,
				Description: "Capability ref the grant authorizes.",
			},
			"environment": schema.StringAttribute{
				Required:    true,
				Description: "Execution environment slug the grant applies to.",
			},
			"resource_glob": schema.StringAttribute{
				Optional:    true,
				Description: "Optional resource glob the grant scopes to. Defaults server-side to \"**\".",
			},
			"requires_approval": schema.BoolAttribute{
				Optional:    true,
				Description: "Whether invocations under this grant require approval.",
			},
			"profile": schema.StringAttribute{
				Optional:    true,
				Description: "Optional authorization profile applied to the grant.",
			},
			"constraints": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional constraints as a JSON object.",
			},
		},
	}
}

func (r *grantResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

func (m grantModel) toSpec() (map[string]any, error) {
	spec := map[string]any{
		"subjectKind": m.SubjectKind.ValueString(),
		"subjectRef":  m.SubjectRef.ValueString(),
		"capability":  m.Capability.ValueString(),
		"environment": m.Environment.ValueString(),
	}
	if s := optString(m.ResourceGlob); s != "" {
		spec["resourceGlob"] = s
	}
	if !m.RequiresApproval.IsNull() && !m.RequiresApproval.IsUnknown() {
		spec["requiresApproval"] = m.RequiresApproval.ValueBool()
	}
	if s := optString(m.Profile); s != "" {
		spec["profile"] = s
	}
	v, err := normalizedToValue(m.Constraints)
	if err != nil {
		return nil, err
	}
	if v != nil {
		spec["constraints"] = v
	}
	return spec, nil
}

func (m *grantModel) applyReadback(spec map[string]any) {
	m.SubjectKind = types.StringValue(specString(spec, "subjectKind"))
	m.SubjectRef = types.StringValue(specString(spec, "subjectRef"))
	m.Capability = types.StringValue(specString(spec, "capability"))
	m.Environment = types.StringValue(specString(spec, "environment"))
	m.ResourceGlob = strOrNull(specString(spec, "resourceGlob"))
	m.Profile = strOrNull(specString(spec, "profile"))
	if v, ok := spec["requiresApproval"].(bool); ok {
		m.RequiresApproval = types.BoolValue(v)
	} else {
		m.RequiresApproval = types.BoolNull()
	}
	m.Constraints = specNormalized(spec, "constraints")
}

func (r *grantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan grantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid constraints", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, grantKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create grant", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, grantKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read grant after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *grantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state grantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, grantKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read grant", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *grantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan grantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid constraints", err.Error())
		return
	}
	res, err := r.client.Update(ctx, grantKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update grant", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *grantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state grantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, grantKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete grant", err.Error())
	}
}

func (r *grantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
