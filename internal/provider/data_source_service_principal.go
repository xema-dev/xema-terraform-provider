// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

var (
	_ datasource.DataSource              = (*servicePrincipalDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*servicePrincipalDataSource)(nil)
)

type servicePrincipalDataSource struct {
	client *client.Client
}

// NewServicePrincipalDataSource is the factory registered with the provider.
func NewServicePrincipalDataSource() datasource.DataSource {
	return &servicePrincipalDataSource{}
}

// servicePrincipalDataModel is the read-only view of a principal; it never
// carries a secret.
type servicePrincipalDataModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	DisplayName        types.String `tfsdk:"display_name"`
	Description        types.String `tfsdk:"description"`
	ClientID           types.String `tfsdk:"client_id"`
	Audience           types.String `tfsdk:"audience"`
	TokenEndpoint      types.String `tfsdk:"token_endpoint"`
	CredentialRevision types.String `tfsdk:"credential_revision"`
	State              types.String `tfsdk:"state"`
}

func (d *servicePrincipalDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_principal"
}

func (d *servicePrincipalDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing service principal by id. It exposes the client id and token endpoint, never a secret.",
		Attributes: map[string]schema.Attribute{
			"id":                  schema.StringAttribute{Required: true, Description: "The principal id."},
			"name":                schema.StringAttribute{Computed: true, Description: "Unique name of the principal."},
			"display_name":        schema.StringAttribute{Computed: true, Description: "Human-readable name."},
			"description":         schema.StringAttribute{Computed: true, Description: "What the principal is for."},
			"client_id":           schema.StringAttribute{Computed: true, Description: "The OAuth client id."},
			"audience":            schema.StringAttribute{Computed: true, Description: "The one audience the client credential is bound to; request tokens with `scope=audience:<audience>`. Null when the service reports none: then do not build the scope from it."},
			"token_endpoint":      schema.StringAttribute{Computed: true, Description: "The OAuth token endpoint."},
			"credential_revision": schema.StringAttribute{Computed: true, Description: "The revision of the current credential."},
			"state":               schema.StringAttribute{Computed: true, Description: "Lifecycle state reported by the service."},
		},
	}
}

func (d *servicePrincipalDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *servicePrincipalDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data servicePrincipalDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, servicePrincipalKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read service principal", err.Error())
		return
	}
	data.Name = types.StringValue(specString(res.Spec, "name"))
	data.DisplayName = types.StringValue(specString(res.Spec, "displayName"))
	data.Description = strOrNull(specString(res.Spec, "description"))
	data.ClientID = strOrNull(statusString(res.Status, "clientId"))
	data.Audience = strOrNull(statusString(res.Status, "audience"))
	data.TokenEndpoint = strOrNull(statusString(res.Status, "tokenEndpoint"))
	data.CredentialRevision = strOrNull(statusString(res.Status, "credentialRevision"))
	data.State = strOrNull(statusString(res.Status, "state"))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
