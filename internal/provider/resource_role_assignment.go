// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// roleAssignmentKind is the XemaResourceKind wire value for role assignments.
const roleAssignmentKind = "role-assignment"

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

// roleAssignmentPrincipalModel is the principal holding the role: `{ kind, ref }`.
type roleAssignmentPrincipalModel struct {
	Kind types.String `tfsdk:"kind"`
	Ref  types.String `tfsdk:"ref"`
}

// roleAssignmentModel mirrors the role-assignment kind spec { definitionId,
// principal, scopeRef?, environmentRef?, until? }: one role definition bound to
// one principal. The id is the binding id the server mints. The server has no
// update, so every attribute replaces the resource.
type roleAssignmentModel struct {
	ID             types.String                  `tfsdk:"id"`
	DefinitionID   types.String                  `tfsdk:"definition_id"`
	Principal      *roleAssignmentPrincipalModel `tfsdk:"principal"`
	ScopeRef       types.String                  `tfsdk:"scope_ref"`
	EnvironmentRef types.String                  `tfsdk:"environment_ref"`
	Until          types.String                  `tfsdk:"until"`
}

func (r *roleAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

func (r *roleAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "One role definition held by one principal in the organization, optionally at a scope " +
			"and in an environment, optionally until a time. Managed through authorization-api's bindings " +
			"via the control plane, with the credential running Terraform: the route accepts a person's " +
			"token only and refuses a service token by design, so an admin runs the assignment. " +
			"A binding cannot be updated, so any change replaces it. Import with the binding id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The binding id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"definition_id": schema.StringAttribute{
				Required:      true,
				Description:   "Id of the role definition to assign (`xema_role.<name>.id`).",
				PlanModifiers: replace,
			},
			"principal": schema.SingleNestedAttribute{
				Required:      true,
				Description:   "The principal that holds the role.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"kind": schema.StringAttribute{
						Required:    true,
						Description: "Kind of principal, e.g. `service` or `user`; authorization-api refuses a kind it does not know.",
					},
					"ref": schema.StringAttribute{
						Required:    true,
						Description: "The principal's id, e.g. a service identity's principal id.",
					},
				},
			},
			"scope_ref": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Scope the role is held at. Unset is the organization root, which reads back here.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"environment_ref": schema.StringAttribute{
				Optional:      true,
				Description:   "Environment the role is held in; unset for every environment.",
				PlanModifiers: replace,
			},
			"until": schema.StringAttribute{
				Optional:      true,
				Description:   "RFC 3339 date-time after which the assignment stops holding; unset for no end.",
				PlanModifiers: replace,
			},
		},
	}
}

func (r *roleAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (m roleAssignmentModel) toSpec() map[string]any {
	spec := map[string]any{"definitionId": m.DefinitionID.ValueString()}
	if m.Principal != nil {
		spec["principal"] = map[string]any{
			"kind": m.Principal.Kind.ValueString(),
			"ref":  m.Principal.Ref.ValueString(),
		}
	}
	if v := optString(m.ScopeRef); v != "" {
		spec["scopeRef"] = v
	}
	if v := optString(m.EnvironmentRef); v != "" {
		spec["environmentRef"] = v
	}
	if v := optString(m.Until); v != "" {
		spec["until"] = v
	}
	return spec
}

// sameInstant reports whether two RFC 3339 strings name the same time, so the
// server's ISO form of `until` does not read as a change from the declared one.
func sameInstant(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	return errA == nil && errB == nil && ta.Equal(tb)
}

// applyReadback sets the model from the server's spec; the declared `until` is
// kept when the server's value is the same instant.
func (m *roleAssignmentModel) applyReadback(spec map[string]any) {
	m.DefinitionID = types.StringValue(specString(spec, "definitionId"))
	principal, _ := spec["principal"].(map[string]any)
	m.Principal = &roleAssignmentPrincipalModel{
		Kind: types.StringValue(specString(principal, "kind")),
		Ref:  types.StringValue(specString(principal, "ref")),
	}
	m.ScopeRef = types.StringValue(specString(spec, "scopeRef"))
	m.EnvironmentRef = strOrNull(specString(spec, "environmentRef"))
	live := specString(spec, "until")
	if live != "" && !m.Until.IsNull() && !m.Until.IsUnknown() && sameInstant(live, m.Until.ValueString()) {
		return
	}
	m.Until = strOrNull(live)
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
		"A role assignment has no field to change; a different definition, principal, scope, environment or end replaces the resource.")
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
