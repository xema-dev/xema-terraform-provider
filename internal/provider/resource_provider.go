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
	_ resource.Resource                = (*providerResource)(nil)
	_ resource.ResourceWithConfigure   = (*providerResource)(nil)
	_ resource.ResourceWithImportState = (*providerResource)(nil)
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
type providerSpecModel struct {
	ID                    types.String `tfsdk:"id"`
	Name                  types.String `tfsdk:"name"`
	Slug                  types.String `tfsdk:"slug"`
	APIType               types.String `tfsdk:"api_type"`
	BaseURL               types.String `tfsdk:"base_url"`
	AuthType              types.String `tfsdk:"auth_type"`
	APIKey                types.String `tfsdk:"api_key"`
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
				Optional:    true,
				Sensitive:   true,
				Description: "Provider API key. Write-only: the service never returns it, so it is not drift-detected.",
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
func (m *providerSpecModel) applyReadback(spec map[string]any) {
	m.Name = types.StringValue(specString(spec, "name"))
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.APIType = types.StringValue(specString(spec, "apiType"))
	m.BaseURL = types.StringValue(specString(spec, "baseUrl"))
	m.AuthType = strOrNull(specString(spec, "authType"))

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
