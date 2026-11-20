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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// roleAssignmentKind is the XemaResourceKind wire value for role assignments.
const roleAssignmentKind = "role-assignment"

// roleAssignmentSubjectKinds is authorization-api's SubjectKind, the closed set
// the role-assignment kind accepts in `subjectKind`.
var roleAssignmentSubjectKinds = []string{
	"user", "agent", "app", "app-client", "service", "group", "role", "runner",
	"external-subject", "anon", "organization", "org-administrators",
}

var (
	_ resource.Resource                = (*roleAssignmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*roleAssignmentResource)(nil)
	_ resource.ResourceWithImportState = (*roleAssignmentResource)(nil)
)

type roleAssignmentResource struct {
	client *client.Client
}

// NewRoleAssignmentResource is the factory registered with the provider.
func NewRoleAssignmentResource() resource.Resource {
	return &roleAssignmentResource{}
}

// roleAssignmentModel mirrors the role-assignment kind spec { roleId,
// subjectKind, subjectRef }. All three name the assignment and none configures
// it, so the id is the key itself: `<roleId>:<subjectKind>:<subjectRef>`.
type roleAssignmentModel struct {
	ID          types.String `tfsdk:"id"`
	RoleID      types.String `tfsdk:"role_id"`
	SubjectKind types.String `tfsdk:"subject_kind"`
	SubjectRef  types.String `tfsdk:"subject_ref"`
}

func (r *roleAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

func (r *roleAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "One org role held by one subject in the organization, e.g. a pipeline's service " +
			"identity holding a custom role. Managed through authorization-api's org-admin role routes " +
			"via the control plane, with the credential running Terraform: the route accepts a person's " +
			"token only and refuses a service token by design, so an admin runs the assignment. " +
			"Every attribute names the assignment, so any change replaces it. Import with " +
			"`<role_id>:<subject_kind>:<subject_ref>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "`<role_id>:<subject_kind>:<subject_ref>` (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"role_id": schema.StringAttribute{
				Required:      true,
				Description:   "Id of the role to assign: a built-in role or one of the org's custom roles (`xema_role.<name>.id`).",
				PlanModifiers: replace,
			},
			"subject_kind": schema.StringAttribute{
				Required:      true,
				Description:   "Kind of subject that holds the role, e.g. `service` or `user`.",
				PlanModifiers: replace,
				Validators:    []validator.String{stringOneOfValidator{allowed: roleAssignmentSubjectKinds}},
			},
			"subject_ref": schema.StringAttribute{
				Required:      true,
				Description:   "The subject's id, e.g. a service identity's principal id.",
				PlanModifiers: replace,
			},
		},
	}
}

func (r *roleAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (m roleAssignmentModel) toSpec() map[string]any {
	return map[string]any{
		"roleId":      m.RoleID.ValueString(),
		"subjectKind": m.SubjectKind.ValueString(),
		"subjectRef":  m.SubjectRef.ValueString(),
	}
}

func (m *roleAssignmentModel) applyReadback(spec map[string]any) {
	m.RoleID = types.StringValue(specString(spec, "roleId"))
	m.SubjectKind = types.StringValue(specString(spec, "subjectKind"))
	m.SubjectRef = types.StringValue(specString(spec, "subjectRef"))
}

func (r *roleAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	handle, err := r.client.Create(ctx, roleAssignmentKind, plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to assign role", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, roleAssignmentKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read role assignment after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, roleAssignmentKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read role assignment", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never planned: every attribute requires replacement. It refuses
// rather than write, as the control-plane kind does.
func (r *roleAssignmentResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Role assignments are immutable",
		"A role assignment has no field to change; a different role or subject replaces the resource.")
}

// Delete withdraws the assignment; one that is already gone is done.
func (r *roleAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, roleAssignmentKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to remove role assignment", err.Error())
	}
}

func (r *roleAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
