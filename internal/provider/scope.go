// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import "github.com/xema-dev/xema-terraform-provider/internal/client"

// resourceScope is the closed set of authority a resource or data source needs.
type resourceScope int

const (
	// orgScoped resources act inside one org and need the provider's `org`.
	orgScoped resourceScope = iota
	// operatorScoped resources act on the installation and work without `org`.
	operatorScoped
)

const orgScopedDiagnosticSummary = "Resource requires an org-scoped provider"

const orgScopedDiagnosticDetail = "this resource is org-scoped; configure the provider with org " +
	"(the `org` attribute or the XEMA_ORG environment variable). Without org the provider runs in " +
	"operator mode, which only serves installation-scoped resources such as xema_org."

// diagnosticsAdder is the error sink shared by resource and data source responses.
type diagnosticsAdder interface {
	AddError(summary, detail string)
}

// clientFromProviderData is the one place that turns provider data into a
// client for a resource or data source. It rejects an org-scoped consumer when
// the provider runs in operator mode, so no resource re-implements the check.
// Returns nil during the early Configure pass when providerData is not yet set.
func clientFromProviderData(providerData any, scope resourceScope, diags diagnosticsAdder) *client.Client {
	if providerData == nil {
		return nil
	}
	c, ok := providerData.(*client.Client)
	if !ok {
		diags.AddError("Unexpected provider data",
			"Expected *client.Client. This is a bug in the provider; please report it.")
		return nil
	}
	if scope == orgScoped && !c.HasOrg() {
		diags.AddError(orgScopedDiagnosticSummary, orgScopedDiagnosticDetail)
		return nil
	}
	return c
}
