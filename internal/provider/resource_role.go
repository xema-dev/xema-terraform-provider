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

// roleKind is the XemaResourceKind wire value for roles (permission sets).
const roleKind = "role"

var (
	_ resource.Resource                = (*roleResource)(nil)
	_ resource.ResourceWithConfigure   = (*roleResource)(nil)
	_ resource.ResourceWithImportState = (*roleResource)(nil)
)

type roleResource struct {
	client *client.Client
}

// NewRoleResource is the factory registered with the provider.
func NewRoleResource() resource.Resource {
	return &roleResource{}
}

// roleModel mirrors the role kind spec (slug, displayName, description, grants)
// plus the server-owned physical id stored in state. The slug is the immutable
// natural key; built-in roles are not manageable here. Who holds the role is
// not part of it: that is `xema_role_assignment`.
type roleModel struct {
	ID          types.String     `tfsdk:"id"`
	Slug        types.String     `tfsdk:"slug"`
	DisplayName types.String     `tfsdk:"display_name"`
	Description types.String     `tfsdk:"description"`
	Grants      []roleGrantModel `tfsdk:"grants"`
}

func (r *roleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *roleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Xema custom role (permission set) and the grants it holds, managed through " +
			"authorization-api via the control plane. Who holds the role is declared with `xema_role_assignment`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted role id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "Role slug (org-unique, kebab-case). Immutable natural key.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable role name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Free-form role description.",
			},
			"grants": schema.SetNestedAttribute{
				Optional:     true,
				Description:  roleGrantsDescription,
				NestedObject: schema.NestedAttributeObject{Attributes: roleGrantAttributes()},
			},
		},
	}
}

func (r *roleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

// toSpec always carries `grants`: the role kind reads an absent `grants` as the
// empty set, so a declaration that left it out would withdraw every grant on
// each update. Sending it makes that the configuration's statement, not an
// accident of omission.
func (m roleModel) toSpec() (map[string]any, error) {
	grants, err := roleGrantsToSpec(m.Grants)
	if err != nil {
		return nil, err
	}
	spec := map[string]any{
		"slug":        m.Slug.ValueString(),
		"displayName": m.DisplayName.ValueString(),
		"grants":      grants,
	}
	if d := optString(m.Description); d != "" {
		spec["description"] = d
	}
	return spec, nil
}

// applyReadback sets the model from the server's spec; prior is the grant set
// the plan or state held, kept where the server's value means the same.
func (m *roleModel) applyReadback(spec map[string]any, prior []roleGrantModel) {
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.DisplayName = types.StringValue(specString(spec, "displayName"))
	m.Description = strOrNull(specString(spec, "description"))
	m.Grants = roleGrantsFromSpec(spec, prior)
}

func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid role grants", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, roleKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create role", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, roleKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read role after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec, plan.Grants)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, roleKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read role", err.Error())
		return
	}
	state.applyReadback(res.Spec, state.Grants)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *roleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid role grants", err.Error())
		return
	}
	res, err := r.client.Update(ctx, roleKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update role", err.Error())
		return
	}
	plan.applyReadback(res.Spec, plan.Grants)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, roleKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete role", err.Error())
	}
}

func (r *roleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
