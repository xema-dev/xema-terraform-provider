// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

// Package provider implements the Xema Terraform provider on the
// terraform-plugin-framework. It exposes the control-plane-api's wired managed
// resource kinds (project, provider, model-resolution-rule) as Terraform
// resources.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// Environment variables used as fallbacks for the provider configuration.
const (
	envEndpoint = "XEMA_ENDPOINT"
	envOrg      = "XEMA_ORG"
	envToken    = "XEMA_TOKEN"
)

// Ensure xemaProvider satisfies the framework interface.
var _ provider.Provider = (*xemaProvider)(nil)

// xemaProvider is the provider implementation.
type xemaProvider struct {
	version string
}

// New returns a provider factory bound to a build version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &xemaProvider{version: version}
	}
}

// providerModel maps the provider configuration block.
type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Org      types.String `tfsdk:"org"`
	Token    types.String `tfsdk:"token"`
}

func (p *xemaProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "xema"
	resp.Version = p.version
}

func (p *xemaProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Provision Xema platform resources as code through the Xema control-plane-api.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Base URL of the Xema control-plane-api. May also be set via the XEMA_ENDPOINT environment variable.",
			},
			"org": schema.StringAttribute{
				Optional:    true,
				Description: "Xema organization id. May also be set via the XEMA_ORG environment variable.",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Org-admin bearer token. May also be set via the XEMA_TOKEN environment variable.",
			},
		},
	}
}

func (p *xemaProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := resolve(cfg.Endpoint, envEndpoint)
	org := resolve(cfg.Org, envOrg)
	token := resolve(cfg.Token, envToken)

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(pathOf("endpoint"),
			"Missing control-plane endpoint",
			"Set the `endpoint` provider attribute or the XEMA_ENDPOINT environment variable.")
	}
	if org == "" {
		resp.Diagnostics.AddAttributeError(pathOf("org"),
			"Missing organization id",
			"Set the `org` provider attribute or the XEMA_ORG environment variable.")
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(pathOf("token"),
			"Missing org-admin token",
			"Set the `token` provider attribute or the XEMA_TOKEN environment variable.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	c := client.New(endpoint, org, token, nil)
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *xemaProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewProjectResource,
		NewProviderResource,
		NewModelResolutionRuleResource,
		NewRoleResource,
		NewOrgResource,
		NewDeliverableSpecResource,
		NewBiomeInstallResource,
		NewPortalResource,
	}
}

func (p *xemaProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewProjectDataSource,
	}
}

// resolve returns the configured value if known and non-empty, else the env var.
func resolve(v types.String, env string) string {
	if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		return v.ValueString()
	}
	return os.Getenv(env)
}
