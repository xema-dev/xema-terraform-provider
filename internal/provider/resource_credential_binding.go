// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// credentialBindingKind is the XemaResourceKind wire value of a credential binding.
const credentialBindingKind = "credential-binding"

var (
	_ resource.Resource                = (*credentialBindingResource)(nil)
	_ resource.ResourceWithConfigure   = (*credentialBindingResource)(nil)
	_ resource.ResourceWithImportState = (*credentialBindingResource)(nil)
)

type credentialBindingResource struct {
	client *client.Client
}

// NewCredentialBindingResource is the factory registered with the provider.
func NewCredentialBindingResource() resource.Resource {
	return &credentialBindingResource{}
}

// llmProviderModel is the `{ slug, baseUrl }` the server derives a provider key from.
type llmProviderModel struct {
	Slug    types.String `tfsdk:"slug"`
	BaseURL types.String `tfsdk:"base_url"`
}

func (m *llmProviderModel) toSpec() map[string]any {
	return map[string]any{"slug": m.Slug.ValueString(), "baseUrl": m.BaseURL.ValueString()}
}

// llmProviderAttribute is the config-only `llm_provider` object shared by the
// credential kinds. The server does not read it back, so it is never refreshed;
// a change replaces the resource, except on a resource imported without it
// (state null), which adopts the declared value in place.
func llmProviderAttribute(description string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Required:    true,
		Description: description,
		Attributes: map[string]schema.Attribute{
			"slug":     schema.StringAttribute{Required: true, Description: "The LLM provider slug."},
			"base_url": schema.StringAttribute{Required: true, Description: "The provider's inference endpoint base URL."},
		},
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.ObjectRequest, resp *objectplanmodifier.RequiresReplaceIfFuncResponse) {
				resp.RequiresReplace = !req.StateValue.IsNull()
			}, "A changed `llm_provider` replaces the resource.", "A changed `llm_provider` replaces the resource."),
		},
	}
}

// credentialBindingModel mirrors the credential-binding kind. secret_value is
// write-only on the wire: the server never returns it.
type credentialBindingModel struct {
	ID                  types.String      `tfsdk:"id"`
	ScopeTier           types.String      `tfsdk:"scope_tier"`
	ProjectID           types.String      `tfsdk:"project_id"`
	LLMProvider         *llmProviderModel `tfsdk:"llm_provider"`
	AuthType            types.String      `tfsdk:"auth_type"`
	HeaderName          types.String      `tfsdk:"header_name"`
	AllowedCapabilities types.List        `tfsdk:"allowed_capabilities"`
	AllowedActions      types.List        `tfsdk:"allowed_actions"`
	AllowedResources    types.List        `tfsdk:"allowed_resources"`
	SecretValue         types.String      `tfsdk:"secret_value"`
	SecretVersion       types.String      `tfsdk:"secret_version"`
	ProviderKey         types.String      `tfsdk:"provider_key"`
	Version             types.Int64       `tfsdk:"version"`
}

func (r *credentialBindingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_credential_binding"
}

func requiredStringList(description string) schema.ListAttribute {
	return schema.ListAttribute{Required: true, ElementType: types.StringType, Description: description}
}

func (r *credentialBindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "An organisation- or project-tier `model_provider` credential binding in the credential " +
			"broker: the sealed key for one LLM provider endpoint, with the allow-lists that bound its use. " +
			"The id is the broker's binding id; import an existing binding with it. The scope, project, " +
			"auth type and provider cannot change: a change replaces the binding. The allow-lists and " +
			"`header_name` update in place. Destroying the resource revokes the binding; a binding " +
			"already revoked or gone is a successful destroy. Pair it with `xema_credential_default` to make " +
			"it the default for the provider.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The credential broker's binding id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"scope_tier": schema.StringAttribute{
				Required:      true,
				Description:   "`org` or `project`.",
				PlanModifiers: replace,
			},
			"project_id": schema.StringAttribute{
				Optional:      true,
				Description:   "The project id; required for `project`, refused for `org`.",
				PlanModifiers: replace,
			},
			"llm_provider": llmProviderAttribute("The LLM provider `{ slug, base_url }` the binding's key is derived from. " +
				"Config-only: the server does not return it, so it is not refreshed."),
			"auth_type": schema.StringAttribute{
				Required:      true,
				Description:   "How the endpoint takes the key: `bearer` or `api_key`.",
				PlanModifiers: replace,
			},
			"header_name": schema.StringAttribute{
				Optional:    true,
				Description: "The header carrying the key; required for `api_key`, refused for `bearer`.",
			},
			"allowed_capabilities": requiredStringList("Capabilities the binding may be used for (non-empty)."),
			"allowed_actions":      requiredStringList("Actions the binding may be used for (non-empty)."),
			"allowed_resources":    requiredStringList("Resources the binding may be used on (non-empty)."),
			"secret_value": schema.StringAttribute{
				Required:  true,
				Sensitive: true,
				Description: "The provider key. Sensitive and write-only on the wire: it is sent on create and " +
					"when `secret_version` changes, and the service never returns it. This provider's " +
					"framework version has no write-only attribute support, so Terraform still keeps the " +
					"configured value in state (as `xema_provider.api_key` does): the state backend holds a " +
					"secret. Changing this value alone never rotates the secret; change `secret_version`.",
			},
			"secret_version": schema.StringAttribute{
				Optional: true,
				Description: "A label you choose for the secret. Change it, together with `secret_value`, to " +
					"rotate the secret in place. While it is unchanged, `secret_value` is never sent, so an " +
					"ordinary re-apply does not rotate.",
			},
			"provider_key": schema.StringAttribute{
				Computed:    true,
				Description: "Read-only: the provider key the server derived from `llm_provider`.",
			},
			"version": schema.Int64Attribute{
				Computed:    true,
				Description: "Read-only: the binding's version, advanced by each in-place change or rotation.",
			},
		},
	}
}

func (r *credentialBindingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

// toSpec builds the spec. The secret is included only when withSecret is set.
func (m credentialBindingModel) toSpec(withSecret bool) map[string]any {
	spec := map[string]any{
		"scopeTier":           m.ScopeTier.ValueString(),
		"authType":            m.AuthType.ValueString(),
		"allowedCapabilities": stringListToSpec(m.AllowedCapabilities),
		"allowedActions":      stringListToSpec(m.AllowedActions),
		"allowedResources":    stringListToSpec(m.AllowedResources),
	}
	if m.LLMProvider != nil {
		spec["llmProvider"] = m.LLMProvider.toSpec()
	}
	if v := optString(m.ProjectID); v != "" {
		spec["projectId"] = v
	}
	if v := optString(m.HeaderName); v != "" {
		spec["headerName"] = v
	}
	if withSecret {
		if v := optString(m.SecretValue); v != "" {
			spec["secretValue"] = v
		}
	}
	return spec
}

// applyReadback refreshes the server-owned fields and leaves the config-only
// ones (llm_provider, secret_value, secret_version) as they are.
func (m *credentialBindingModel) applyReadback(res *client.Resource) {
	m.ID = types.StringValue(res.PhysicalID)
	m.ScopeTier = types.StringValue(specString(res.Spec, "scopeTier"))
	m.ProjectID = strOrNull(specString(res.Spec, "projectId"))
	m.AuthType = types.StringValue(specString(res.Spec, "authType"))
	m.HeaderName = strOrNull(specString(res.Spec, "headerName"))
	m.AllowedCapabilities = specToStringList(res.Spec, "allowedCapabilities")
	m.AllowedActions = specToStringList(res.Spec, "allowedActions")
	m.AllowedResources = specToStringList(res.Spec, "allowedResources")
	m.ProviderKey = strOrNull(specString(res.Status, "providerKey"))
	if v, ok := res.Status["version"].(float64); ok {
		m.Version = types.Int64Value(int64(v))
	} else {
		m.Version = types.Int64Null()
	}
}

func (r *credentialBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan credentialBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	handle, err := r.client.Create(ctx, credentialBindingKind, plan.toSpec(true))
	if err != nil {
		resp.Diagnostics.AddError("Failed to create credential binding", err.Error())
		return
	}
	r.finish(ctx, &plan, handle.PhysicalID, &resp.Diagnostics, func(m *credentialBindingModel) diag.Diagnostics {
		return resp.State.Set(ctx, m)
	})
}

// finish reads the binding back and stores it, keeping the config-only fields from plan.
func (r *credentialBindingResource) finish(ctx context.Context, plan *credentialBindingModel, id string, diags *diag.Diagnostics, set func(*credentialBindingModel) diag.Diagnostics) {
	res, err := r.client.Read(ctx, credentialBindingKind, id)
	if err != nil {
		diags.AddError("Failed to read credential binding after write", err.Error())
		return
	}
	plan.applyReadback(res)
	diags.Append(set(plan)...)
}

func (r *credentialBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state credentialBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.client.Read(ctx, credentialBindingKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read credential binding", err.Error())
		return
	}
	state.applyReadback(res)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *credentialBindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state credentialBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The secret travels only when the version moved: a re-apply never rotates.
	rotate := !plan.SecretVersion.Equal(state.SecretVersion)
	id := state.ID.ValueString()
	if _, err := r.client.Update(ctx, credentialBindingKind, id, plan.toSpec(rotate)); err != nil {
		resp.Diagnostics.AddError("Failed to update credential binding", err.Error())
		return
	}
	plan.ID = state.ID
	r.finish(ctx, &plan, id, &resp.Diagnostics, func(m *credentialBindingModel) diag.Diagnostics {
		return resp.State.Set(ctx, m)
	})
}

// Delete revokes the binding. Already revoked or gone (404) is success.
func (r *credentialBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state credentialBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, credentialBindingKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to revoke credential binding", err.Error())
	}
}

func (r *credentialBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
