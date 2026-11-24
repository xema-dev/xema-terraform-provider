// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// credentialDefaultKind is the XemaResourceKind wire value of a credential default.
const credentialDefaultKind = "credential-default"

var (
	_ resource.Resource                = (*credentialDefaultResource)(nil)
	_ resource.ResourceWithConfigure   = (*credentialDefaultResource)(nil)
	_ resource.ResourceWithImportState = (*credentialDefaultResource)(nil)
)

type credentialDefaultResource struct {
	client *client.Client
}

// NewCredentialDefaultResource is the factory registered with the provider.
func NewCredentialDefaultResource() resource.Resource {
	return &credentialDefaultResource{}
}

// credentialDefaultModel mirrors the credential-default kind: the pointer from
// an org's or project's LLM provider to the credential binding that is its default.
type credentialDefaultModel struct {
	ID                  types.String      `tfsdk:"id"`
	ScopeTier           types.String      `tfsdk:"scope_tier"`
	ProjectID           types.String      `tfsdk:"project_id"`
	LLMProvider         *llmProviderModel `tfsdk:"llm_provider"`
	CredentialBindingID types.String      `tfsdk:"credential_binding_id"`
	ProviderKey         types.String      `tfsdk:"provider_key"`
}

func (r *credentialDefaultResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_credential_default"
}

func (r *credentialDefaultResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "The organisation's or a project's default credential for one LLM provider: a pointer to " +
			"a `xema_credential_binding`, written through space-registry as the caller. The id is " +
			"`org:<provider key>` or `project:<project id>:<provider key>`, the provider key being the " +
			"one the server derives from `llm_provider`; import with it. A changed tier, project or " +
			"provider replaces the default; a changed `credential_binding_id` repoints it in place. " +
			"Destroying the resource clears the default, so resolution falls through to the next rung.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "`org:<provider key>` or `project:<project id>:<provider key>`.",
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
			"llm_provider": llmProviderAttribute("The LLM provider `{ slug, base_url }` the default is for. Config-only: " +
				"the server keeps only the derived provider key (`provider_key`)."),
			"credential_binding_id": schema.StringAttribute{
				Required:    true,
				Description: "The id of the `xema_credential_binding` that is the default.",
			},
			"provider_key": schema.StringAttribute{
				Computed:    true,
				Description: "Read-only: the provider key the server derived from `llm_provider`.",
			},
		},
	}
}

func (r *credentialDefaultResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (m credentialDefaultModel) toSpec() map[string]any {
	spec := map[string]any{
		"scopeTier":           m.ScopeTier.ValueString(),
		"credentialBindingId": m.CredentialBindingID.ValueString(),
	}
	if m.LLMProvider != nil {
		spec["llmProvider"] = m.LLMProvider.toSpec()
	}
	if v := optString(m.ProjectID); v != "" {
		spec["projectId"] = v
	}
	return spec
}

// applyReadback refreshes the server-owned fields; llm_provider is config-only
// and kept as it is.
func (m *credentialDefaultModel) applyReadback(res *client.Resource) {
	m.ID = types.StringValue(res.PhysicalID)
	m.ScopeTier = types.StringValue(specString(res.Spec, "scopeTier"))
	m.ProjectID = strOrNull(specString(res.Spec, "projectId"))
	m.CredentialBindingID = types.StringValue(specString(res.Spec, "credentialBindingId"))
	m.ProviderKey = strOrNull(specString(res.Status, "providerKey"))
}

func (r *credentialDefaultResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan credentialDefaultModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	handle, err := r.client.Create(ctx, credentialDefaultKind, plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create credential default", err.Error())
		return
	}
	res, err := r.client.Read(ctx, credentialDefaultKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read credential default after create", err.Error())
		return
	}
	plan.applyReadback(res)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *credentialDefaultResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state credentialDefaultModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.client.Read(ctx, credentialDefaultKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read credential default", err.Error())
		return
	}
	state.applyReadback(res)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *credentialDefaultResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state credentialDefaultModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if _, err := r.client.Update(ctx, credentialDefaultKind, id, plan.toSpec()); err != nil {
		resp.Diagnostics.AddError("Failed to update credential default", err.Error())
		return
	}
	res, err := r.client.Read(ctx, credentialDefaultKind, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read credential default after update", err.Error())
		return
	}
	plan.applyReadback(res)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete clears the default; already gone is success.
func (r *credentialDefaultResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state credentialDefaultModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, credentialDefaultKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to clear credential default", err.Error())
	}
}

func (r *credentialDefaultResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !strings.HasPrefix(req.ID, "org:") && !strings.HasPrefix(req.ID, "project:") {
		resp.Diagnostics.AddError("Invalid import id",
			fmt.Sprintf("import with `org:<provider key>` or `project:<project id>:<provider key>`; got %q.", req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
