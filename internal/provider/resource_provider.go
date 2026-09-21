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

// providerKind is the XemaResourceKind wire value for LLM providers.
const providerKind = "provider"

var (
	_ resource.Resource                   = (*providerResource)(nil)
	_ resource.ResourceWithConfigure      = (*providerResource)(nil)
	_ resource.ResourceWithImportState    = (*providerResource)(nil)
	_ resource.ResourceWithValidateConfig = (*providerResource)(nil)
)

type providerResource struct {
	client *client.Client
}

// NewProviderResource is the factory registered with the provider.
func NewProviderResource() resource.Resource {
	return &providerResource{}
}

// providerSpecModel mirrors the `provider` kind spec served by llm-registry-api
// through the control plane. apiKey is write-only: the service never returns it,
// so it is not refreshed on Read (no drift detection on the secret — by design).
//
// credentialRef is the OTHER credential form and it is NOT write-only: it is an
// opaque pointer rather than a secret, so the service returns it and it IS
// drift-detected. That asymmetry is the point of preferring it in IaC — an
// operator can see that a provider they declared by reference was switched to
// an inline key by hand, which is drift they are entitled to see, and which an
// unreadable api_key can never show.
type providerSpecModel struct {
	ID                    types.String `tfsdk:"id"`
	Name                  types.String `tfsdk:"name"`
	Slug                  types.String `tfsdk:"slug"`
	APIType               types.String `tfsdk:"api_type"`
	BaseURL               types.String `tfsdk:"base_url"`
	AuthType              types.String `tfsdk:"auth_type"`
	APIKey                types.String `tfsdk:"api_key"`
	CredentialRef         types.String `tfsdk:"credential_ref"`
	MaxConcurrentRequests types.Int64  `tfsdk:"max_concurrent_requests"`
	IsActive              types.Bool   `tfsdk:"is_active"`
}

func (r *providerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provider"
}

func (r *providerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An LLM provider, managed through llm-registry-api via the control plane.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted provider id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable provider name.",
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "URL-safe provider slug (unique within the org; the managed key).",
			},
			"api_type": schema.StringAttribute{
				Required:    true,
				Description: "Provider API protocol type (e.g. openai, anthropic).",
			},
			"base_url": schema.StringAttribute{
				Required:    true,
				Description: "Provider API base URL.",
			},
			"auth_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Provider authentication type.",
			},
			"api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "Provider API key. Write-only: the service never returns it, so it is not " +
					"drift-detected. Exactly one of `api_key` or `credential_ref` is required. " +
					"PREFER `credential_ref`: a key set here reaches a tfvars file, a plan and the " +
					"state backend, none of which is where a secret belongs.",
			},
			"credential_ref": schema.StringAttribute{
				Optional: true,
				Description: "Opaque credential-binding id held by the credential plane. Exactly one of " +
					"`api_key` or `credential_ref` is required. The bytes never pass through this " +
					"configuration at all — the service resolves the binding when it needs the key.",
			},
			"max_concurrent_requests": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Maximum concurrent requests permitted against this provider.",
			},
			"is_active": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the provider is active.",
			},
		},
	}
}

func (r *providerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

// ValidateConfig refuses a provider that names NEITHER credential form or BOTH,
// at PLAN time.
//
// The service refuses both cases too — `@ExactlyOneCredentialForm` on the DTO,
// and a CHECK constraint under it — so this is not the fence. It is the
// difference between reading the refusal in `tofu plan`, where the author can
// fix it, and reading it as an API error in the middle of an apply that has
// already created other resources.
//
// Written as ValidateConfig rather than an attribute validator on purpose:
// `stringvalidator.ExactlyOneOf` lives in terraform-plugin-framework-validators,
// which this provider does not depend on, and one cross-field rule does not
// earn a new module in the dependency graph of a provider customers run.
func (r *providerResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg providerSpecModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Unknown at plan time (a value that comes from another resource) is not
	// absent — refusing it would break the ordinary case of a credential
	// binding created in the same apply.
	hasKey := !cfg.APIKey.IsNull() || cfg.APIKey.IsUnknown()
	hasRef := !cfg.CredentialRef.IsNull() || cfg.CredentialRef.IsUnknown()

	switch {
	case hasKey && hasRef:
		resp.Diagnostics.AddError(
			"Exactly one credential form",
			"`api_key` and `credential_ref` are mutually exclusive: a provider holds one "+
				"credential, and two would be two answers with no declared precedence. "+
				"Remove one.",
		)
	case !hasKey && !hasRef:
		resp.Diagnostics.AddError(
			"Exactly one credential form",
			"A provider needs a credential: set `credential_ref` (preferred — the bytes "+
				"stay in the credential plane) or `api_key`. Neither was set, and a "+
				"provider with no credential fails when an agent runs rather than here.",
		)
	}
}

func (m providerSpecModel) toSpec() map[string]any {
	spec := map[string]any{
		"name":    m.Name.ValueString(),
		"slug":    m.Slug.ValueString(),
		"apiType": m.APIType.ValueString(),
		"baseUrl": m.BaseURL.ValueString(),
	}
	if v := optString(m.AuthType); v != "" {
		spec["authType"] = v
	}
	if v := optString(m.APIKey); v != "" {
		spec["apiKey"] = v
	}
	if v := optString(m.CredentialRef); v != "" {
		spec["credentialRef"] = v
	}
	if !m.MaxConcurrentRequests.IsNull() && !m.MaxConcurrentRequests.IsUnknown() {
		spec["maxConcurrentRequests"] = m.MaxConcurrentRequests.ValueInt64()
	}
	if !m.IsActive.IsNull() && !m.IsActive.IsUnknown() {
		spec["isActive"] = m.IsActive.ValueBool()
	}
	return spec
}

// applyReadback refreshes every server-owned field from a read-back spec while
// preserving the write-only api_key already in state/plan.
//
// `credentialRef` IS refreshed, unlike `api_key`, because the service returns
// it: it is a pointer, not a secret. That is what makes a hand-switch from the
// reference form to an inline key visible as drift on the next plan.
func (m *providerSpecModel) applyReadback(spec map[string]any) {
	m.Name = types.StringValue(specString(spec, "name"))
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.APIType = types.StringValue(specString(spec, "apiType"))
	m.BaseURL = types.StringValue(specString(spec, "baseUrl"))
	m.AuthType = strOrNull(specString(spec, "authType"))
	m.CredentialRef = strOrNull(specString(spec, "credentialRef"))

	if v, ok := numberFromSpec(spec, "maxConcurrentRequests"); ok {
		m.MaxConcurrentRequests = types.Int64Value(int64(v))
	} else {
		m.MaxConcurrentRequests = types.Int64Null()
	}
	if v, ok := spec["isActive"].(bool); ok {
		m.IsActive = types.BoolValue(v)
	} else {
		m.IsActive = types.BoolNull()
	}
}

func (r *providerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan providerSpecModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	handle, err := r.client.Create(ctx, providerKind, plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create provider", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	// Read back to populate server-defaulted computed fields (auth_type,
	// max_concurrent_requests, is_active).
	res, err := r.client.Read(ctx, providerKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read provider after create", err.Error())
		return
	}
	apiKey := plan.APIKey
	plan.applyReadback(res.Spec)
	plan.APIKey = apiKey // secret never round-trips
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *providerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state providerSpecModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, providerKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read provider", err.Error())
		return
	}
	apiKey := state.APIKey
	state.applyReadback(res.Spec)
	state.APIKey = apiKey
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *providerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan providerSpecModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Update(ctx, providerKind, plan.ID.ValueString(), plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to update provider", err.Error())
		return
	}
	apiKey := plan.APIKey
	plan.applyReadback(res.Spec)
	plan.APIKey = apiKey
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *providerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state providerSpecModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, providerKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete provider", err.Error())
	}
}

func (r *providerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
