// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// connectionKind is the XemaResourceKind wire value of a connection.
const connectionKind = "connection"

var (
	_ resource.Resource                = (*connectionResource)(nil)
	_ resource.ResourceWithConfigure   = (*connectionResource)(nil)
	_ resource.ResourceWithImportState = (*connectionResource)(nil)
)

type connectionResource struct {
	client *client.Client
}

// NewConnectionResource is the factory registered with the provider.
func NewConnectionResource() resource.Resource {
	return &connectionResource{}
}

// connectionModel mirrors the connection kind. The credential is an existing
// credential binding named by id; no secret is ever part of the model.
type connectionModel struct {
	ID                   types.String `tfsdk:"id"`
	Provider             types.String `tfsdk:"provider_slug"`
	Name                 types.String `tfsdk:"name"`
	ScopeTier            types.String `tfsdk:"scope_tier"`
	CredentialKind       types.String `tfsdk:"credential_kind"`
	Endpoint             types.String `tfsdk:"endpoint"`
	CredentialBindingID  types.String `tfsdk:"credential_binding_id"`
	EndpointRevision     types.Int64  `tfsdk:"endpoint_revision"`
	CredentialsSuspended types.Bool   `tfsdk:"credentials_suspended"`
}

func (r *connectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func (r *connectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A connection to an external provider in the connector gateway, authenticated by an " +
			"existing credential binding named by id (`credential_binding_id`, for example from " +
			"`xema_credential_binding`); the configuration never carries a secret. The provider, name, " +
			"scope tier and credential kind cannot change: a change replaces the connection. The endpoint " +
			"and the credential binding update in place; a changed endpoint suspends the connection's " +
			"credentials, so the binding is re-bound after every endpoint change. The gateway has no " +
			"endpoint clear: once set, an endpoint can only change to another value. Destroying the " +
			"resource deletes the connection (the bound credential binding stays with its owner). Import an " +
			"existing connection by its id; adopting an existing managedKey connection is unsupported, " +
			"because the connection list route refuses managedKey connections.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The connector gateway's connection id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"provider_slug": schema.StringAttribute{
				Required:      true,
				Description:   "The external provider the connection is to. A change replaces the connection.",
				PlanModifiers: replace,
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "The connection's name. A change replaces the connection.",
				PlanModifiers: replace,
			},
			"scope_tier": schema.StringAttribute{
				Required:      true,
				Description:   "The owner tier: `org` or `user`. A change replaces the connection.",
				PlanModifiers: replace,
			},
			"credential_kind": schema.StringAttribute{
				Required: true,
				Description: "The credential kind the connection declares; the bound credential binding must be " +
					"of this kind. A change replaces the connection.",
				PlanModifiers: replace,
			},
			"endpoint": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "The connection's endpoint. Updates in place and re-binds the credential. The " +
					"gateway cannot clear an endpoint, so removing it from the configuration keeps the live one.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"credential_binding_id": schema.StringAttribute{
				Required: true,
				Description: "The id of an existing credential binding (for example " +
					"`xema_credential_binding.example.id`). Updates in place. Reads as drift while the " +
					"connection's credential is absent or suspended.",
			},
			"endpoint_revision": schema.Int64Attribute{
				Computed:    true,
				Description: "Read-only: the endpoint's revision, advanced by each endpoint change. The server reads and sends it; it is not configurable.",
			},
			"credentials_suspended": schema.BoolAttribute{
				Computed:    true,
				Description: "Read-only: whether the connection's credentials are suspended (an endpoint or settings change).",
			},
		},
	}
}

func (r *connectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (m connectionModel) toSpec() map[string]any {
	spec := map[string]any{
		"provider":            m.Provider.ValueString(),
		"name":                m.Name.ValueString(),
		"scopeTier":           m.ScopeTier.ValueString(),
		"credentialKind":      m.CredentialKind.ValueString(),
		"credentialBindingId": m.CredentialBindingID.ValueString(),
	}
	if v := optString(m.Endpoint); v != "" {
		spec["endpoint"] = v
	}
	return spec
}

func (m *connectionModel) applyReadback(res *client.Resource) {
	m.ID = types.StringValue(res.PhysicalID)
	m.Provider = types.StringValue(specString(res.Spec, "provider"))
	m.Name = types.StringValue(specString(res.Spec, "name"))
	m.ScopeTier = types.StringValue(specString(res.Spec, "scopeTier"))
	m.CredentialKind = types.StringValue(specString(res.Spec, "credentialKind"))
	m.Endpoint = strOrNull(specString(res.Spec, "endpoint"))
	// Null while the credential is absent or suspended: it plans as drift.
	m.CredentialBindingID = strOrNull(specString(res.Spec, "credentialBindingId"))
	if v, ok := res.Status["endpointRevision"].(float64); ok {
		m.EndpointRevision = types.Int64Value(int64(v))
	} else {
		m.EndpointRevision = types.Int64Null()
	}
	if v, ok := res.Status["credentialsSuspended"].(bool); ok {
		m.CredentialsSuspended = types.BoolValue(v)
	} else {
		m.CredentialsSuspended = types.BoolValue(false)
	}
}

func (r *connectionResource) finish(ctx context.Context, plan *connectionModel, id string, diags *diag.Diagnostics, set func(*connectionModel) diag.Diagnostics) {
	res, err := r.client.Read(ctx, connectionKind, id)
	if err != nil {
		diags.AddError("Failed to read connection after write", err.Error())
		return
	}
	plan.applyReadback(res)
	diags.Append(set(plan)...)
}

func (r *connectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan connectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	handle, err := r.client.Create(ctx, connectionKind, plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create connection", err.Error())
		return
	}
	r.finish(ctx, &plan, handle.PhysicalID, &resp.Diagnostics, func(m *connectionModel) diag.Diagnostics {
		return resp.State.Set(ctx, m)
	})
}

func (r *connectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state connectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.client.Read(ctx, connectionKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read connection", err.Error())
		return
	}
	state.applyReadback(res)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *connectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state connectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if _, err := r.client.Update(ctx, connectionKind, id, plan.toSpec()); err != nil {
		resp.Diagnostics.AddError("Failed to update connection", err.Error())
		return
	}
	plan.ID = state.ID
	r.finish(ctx, &plan, id, &resp.Diagnostics, func(m *connectionModel) diag.Diagnostics {
		return resp.State.Set(ctx, m)
	})
}

// Delete removes the connection. Already gone (404) is success.
func (r *connectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state connectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, connectionKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete connection", err.Error())
	}
}

// ImportState imports by id only: the list route refuses managedKey connections.
func (r *connectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
