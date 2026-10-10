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
				Optional: true,
				Description: "Xema organization id. May also be set via the XEMA_ORG environment variable. " +
					"Omit it to run in operator mode: only installation-scoped resources (`xema_org`) work, " +
					"and every org-scoped resource or data source fails with a diagnostic.",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Bearer token: org-admin with `org` set, platform-admin in operator mode. May also be set via the XEMA_TOKEN environment variable.",
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
	orgPending := cfg.Org.IsUnknown()
	org := ""
	if !orgPending {
		org = resolve(cfg.Org, envOrg)
	}
	token := resolve(cfg.Token, envToken)

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(pathOf("endpoint"),
			"Missing control-plane endpoint",
			"Set the `endpoint` provider attribute or the XEMA_ENDPOINT environment variable.")
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(pathOf("token"),
			"Missing bearer token",
			"Set the `token` provider attribute or the XEMA_TOKEN environment variable.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// No org (configured or via XEMA_ORG) is operator mode, never a default org.
	// An org that is unknown at plan (fed from another resource) is deferred to
	// apply, where it is known.
	var c *client.Client
	if orgPending {
		c = client.NewOrgPending(endpoint, token, nil)
	} else {
		c = client.New(endpoint, org, token, nil)
	}
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *xemaProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewProjectResource,
		NewProviderResource,
		NewModelResolutionRuleResource,
		NewModelStrategyResource,
		NewOrgModelDefaultResource,
		NewRoleResource,
		NewOrgResource,
		NewDeliverableSpecResource,
		NewBiomeInstallResource,
		NewPortalResource,
		NewAgentResource,
		NewGrantResource,
		NewTeamResource,
		NewEnvironmentResource,
		NewSkillResource,
		NewModelResource,
		NewSpaceResource,
	}
}

func (p *xemaProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewProjectDataSource,
		NewProviderDataSource,
		NewModelDataSource,
		NewModelResolutionRuleDataSource,
		NewModelStrategyDataSource,
		NewOrgModelDefaultDataSource,
		NewAgentDataSource,
		NewSkillDataSource,
		NewRoleDataSource,
		NewGrantDataSource,
		NewTeamDataSource,
		NewEnvironmentDataSource,
		NewSpaceDataSource,
		NewOrgDataSource,
		NewDeliverableSpecDataSource,
		NewBiomeInstallDataSource,
		NewPortalDataSource,
	}
}

// resolve returns the configured value if known and non-empty, else the env var.
func resolve(v types.String, env string) string {
	if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		return v.ValueString()
	}
	return os.Getenv(env)
}
