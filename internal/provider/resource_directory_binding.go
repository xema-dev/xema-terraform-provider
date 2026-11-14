// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// directoryBindingKind is the XemaResourceKind wire value for the organization's
// directory binding: PUT/GET/DELETE on identity-api upstream. It is a singleton
// per organization: physicalId is the org id and managedKey the fixed `default`.
const directoryBindingKind = "directory-binding"

var (
	directoryMemberClaimKinds = []string{"GROUP", "APP_ROLE"}
	directoryOrgRoles         = []string{"org_owner", "org_admin", "org_member"}

	directoryMemberClaimAttrTypes = map[string]attr.Type{
		"kind":   types.StringType,
		"values": types.ListType{ElemType: types.StringType},
	}
	directoryRoleMapAttrTypes = map[string]attr.Type{
		"value":    types.StringType,
		"org_role": types.StringType,
	}
)

var (
	_ resource.Resource                = (*directoryBindingResource)(nil)
	_ resource.ResourceWithConfigure   = (*directoryBindingResource)(nil)
	_ resource.ResourceWithImportState = (*directoryBindingResource)(nil)
)

type directoryBindingResource struct {
	client *client.Client
}

// NewDirectoryBindingResource is the factory registered with the provider.
func NewDirectoryBindingResource() resource.Resource {
	return &directoryBindingResource{}
}

// directoryBindingModel mirrors the directory-binding kind spec. The binding is
// one record per organization, so `id` is the org id.
type directoryBindingModel struct {
	ID                  types.String `tfsdk:"id"`
	ProviderRef         types.String `tfsdk:"provider_ref"`
	ApplicationClientID types.String `tfsdk:"application_client_id"`
	MemberClaim         types.Object `tfsdk:"member_claim"`
	RoleMap             types.List   `tfsdk:"role_map"`
	SCIMCredentialID    types.String `tfsdk:"scim_credential_id"`
	Authority           types.String `tfsdk:"authority"`
}

// stringOneOfValidator refuses a string outside a closed set at plan time. It is
// written against the framework's validator interface directly: this provider
// does not depend on terraform-plugin-framework-validators.
type stringOneOfValidator struct{ allowed []string }

func (v stringOneOfValidator) Description(context.Context) string {
	return "value must be one of: " + strings.Join(v.allowed, ", ")
}

func (v stringOneOfValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v stringOneOfValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	for _, a := range v.allowed {
		if got == a {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid value",
		fmt.Sprintf("Value must be one of %s; got %q.", strings.Join(v.allowed, ", "), got))
}

// listMinOneValidator refuses an empty list at plan time.
type listMinOneValidator struct{}

func (listMinOneValidator) Description(context.Context) string {
	return "list must have at least 1 element"
}

func (v listMinOneValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (listMinOneValidator) ValidateList(_ context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if len(req.ConfigValue.Elements()) == 0 {
		resp.Diagnostics.AddAttributeError(req.Path, "Empty list", "The list must have at least 1 element.")
	}
}

func (r *directoryBindingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_directory_binding"
}

func (r *directoryBindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The organization's directory binding: ties the org to an external identity directory " +
			"(for example Microsoft Entra) and maps directory claims to org roles. One per organization; " +
			"every change is an in-place update (the write replaces the binding). Destroying it removes " +
			"the binding and leaves existing memberships untouched. Managed through identity-api via the " +
			"control plane. Import with the organization id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Control-plane physical id of the binding: the organization id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"provider_ref": schema.StringAttribute{
				Required:    true,
				Description: "Directory provider reference, e.g. `entra:<tenant id>`.",
			},
			"application_client_id": schema.StringAttribute{
				Required:    true,
				Description: "Client id of the directory application the binding trusts.",
			},
			"member_claim": schema.SingleNestedAttribute{
				Required:    true,
				Description: "The directory claim that marks an organization member.",
				Attributes: map[string]schema.Attribute{
					"kind": schema.StringAttribute{
						Required:    true,
						Description: "`GROUP` or `APP_ROLE`.",
						Validators:  []validator.String{stringOneOfValidator{allowed: directoryMemberClaimKinds}},
					},
					"values": schema.ListAttribute{
						Required:    true,
						ElementType: types.StringType,
						Description: "Claim values that grant membership (at least one).",
						Validators:  []validator.List{listMinOneValidator{}},
					},
				},
			},
			"role_map": schema.ListNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "Maps a member claim value to an org role. The server requires one entry per " +
					"member claim value; absent and empty are the same.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"value": schema.StringAttribute{
							Required:    true,
							Description: "A member claim value.",
						},
						"org_role": schema.StringAttribute{
							Required:    true,
							Description: "`org_owner`, `org_admin` or `org_member`.",
							Validators:  []validator.String{stringOneOfValidator{allowed: directoryOrgRoles}},
						},
					},
				},
			},
			"scim_credential_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Optional uuid of the SCIM credential linked to the binding.",
			},
			"authority": schema.StringAttribute{
				Computed:    true,
				Description: "Read-only: who owns membership, `claims` or `scim`.",
			},
		},
	}
}

func (r *directoryBindingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (m directoryBindingModel) toSpec() map[string]any {
	spec := map[string]any{
		"providerRef":         m.ProviderRef.ValueString(),
		"applicationClientId": m.ApplicationClientID.ValueString(),
	}

	claim := map[string]any{}
	attrs := m.MemberClaim.Attributes()
	if k, ok := attrs["kind"].(types.String); ok {
		claim["kind"] = k.ValueString()
	}
	if l, ok := attrs["values"].(types.List); ok {
		vals := stringListToSpec(l)
		if vals == nil {
			vals = []any{}
		}
		claim["values"] = vals
	}
	spec["memberClaim"] = claim

	// An unset (null or unknown) role_map is omitted: the server answers its own
	// empty list, which reads back as null.
	if !m.RoleMap.IsNull() && !m.RoleMap.IsUnknown() {
		entries := make([]any, 0, len(m.RoleMap.Elements()))
		for _, e := range m.RoleMap.Elements() {
			obj, ok := e.(types.Object)
			if !ok {
				continue
			}
			a := obj.Attributes()
			value, _ := a["value"].(types.String)
			role, _ := a["org_role"].(types.String)
			entries = append(entries, map[string]any{
				"value":   value.ValueString(),
				"orgRole": role.ValueString(),
			})
		}
		spec["roleMap"] = entries
	}

	if id := optString(m.SCIMCredentialID); id != "" {
		spec["scimCredentialId"] = id
	}
	return spec
}

func (m *directoryBindingModel) applyReadback(spec map[string]any) {
	m.ProviderRef = types.StringValue(specString(spec, "providerRef"))
	m.ApplicationClientID = types.StringValue(specString(spec, "applicationClientId"))

	claim, _ := spec["memberClaim"].(map[string]any)
	values := []attr.Value{}
	for _, v := range stringList(claim, "values") {
		values = append(values, types.StringValue(v))
	}
	m.MemberClaim = types.ObjectValueMust(directoryMemberClaimAttrTypes, map[string]attr.Value{
		"kind":   types.StringValue(specString(claim, "kind")),
		"values": types.ListValueMust(types.StringType, values),
	})

	// The server answers an empty list when no role map was written. Absent and
	// empty are the same: it reads back as null, unless the declaration (the
	// prior value) was an explicit empty list.
	roleType := types.ObjectType{AttrTypes: directoryRoleMapAttrTypes}
	raw, _ := spec["roleMap"].([]any)
	if len(raw) == 0 {
		if !m.RoleMap.IsNull() && !m.RoleMap.IsUnknown() && len(m.RoleMap.Elements()) == 0 {
			m.RoleMap = types.ListValueMust(roleType, []attr.Value{})
		} else {
			m.RoleMap = types.ListNull(roleType)
		}
	} else {
		elems := make([]attr.Value, 0, len(raw))
		for _, e := range raw {
			entry, _ := e.(map[string]any)
			elems = append(elems, types.ObjectValueMust(directoryRoleMapAttrTypes, map[string]attr.Value{
				"value":    types.StringValue(specString(entry, "value")),
				"org_role": types.StringValue(specString(entry, "orgRole")),
			}))
		}
		m.RoleMap = types.ListValueMust(roleType, elems)
	}

	m.SCIMCredentialID = strOrNull(specString(spec, "scimCredentialId"))
	m.Authority = strOrNull(specString(spec, "authority"))
}

func (r *directoryBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan directoryBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	handle, err := r.client.Create(ctx, directoryBindingKind, plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to set directory binding", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, directoryBindingKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read directory binding after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *directoryBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state directoryBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, directoryBindingKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read directory binding", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *directoryBindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan directoryBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Update(ctx, directoryBindingKind, plan.ID.ValueString(), plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to update directory binding", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *directoryBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state directoryBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, directoryBindingKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to remove directory binding", err.Error())
	}
}

func (r *directoryBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
