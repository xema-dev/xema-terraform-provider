// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

var (
	_ datasource.DataSource              = (*distributionLockDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*distributionLockDataSource)(nil)
)

type distributionLockDataSource struct {
	client *client.Client
}

// NewDistributionLockDataSource is the factory registered with the provider.
func NewDistributionLockDataSource() datasource.DataSource {
	return &distributionLockDataSource{}
}

// distributionLockModel resolves a distribution + available-biome index into a
// DistributionLock. Inputs/outputs are normalized JSON, so the (recursive,
// union-shaped) distribution structures round-trip without drift.
type distributionLockModel struct {
	ID                     types.String         `tfsdk:"id"`
	Distribution           jsontypes.Normalized `tfsdk:"distribution"`
	AvailableBiomes        jsontypes.Normalized `tfsdk:"available_biomes"`
	CapabilityDomainOwners jsontypes.Normalized `tfsdk:"capability_domain_owners"`
	PlatformServiceSources jsontypes.Normalized `tfsdk:"platform_service_sources"`
	Lock                   jsontypes.Normalized `tfsdk:"lock"`
}

func (d *distributionLockDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_distribution_lock"
}

func (d *distributionLockDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Resolve a Distribution + available-biome index into a DistributionLock via " +
			"fleet-control-api (operator plane). Side-effect-free. Requires the provider's " +
			"`fleet_endpoint` and a token that satisfies fleet-control's service-actor guard.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Resolved distribution id (echoed from the lock).",
			},
			"distribution": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "The Distribution to resolve, as a JSON object (schemaVersion, id, include[], …).",
			},
			"available_biomes": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "The available-biome index to resolve against, as a JSON array of AvailableBiome objects.",
			},
			"capability_domain_owners": schema.StringAttribute{
				Required:   true,
				CustomType: jsontypes.NormalizedType{},
				Description: "The platform-reserved capability-domain table, as a JSON array of " +
					"{domain, biomeId} objects. Required and non-empty. It is deliberately NOT " +
					"derived from available_biomes: that index is a caller-composed pool, so " +
					"deriving would leave a domain unreserved whenever its owning biome is trimmed " +
					"out, and would let a third-party biome in the pool mint itself a reservation.",
			},
			"platform_service_sources": schema.StringAttribute{
				Required:   true,
				CustomType: jsontypes.NormalizedType{},
				Description: "Exact source identity for every platform service the distribution " +
					"declares, as a JSON array of {name, tier, repo, commit, version, " +
					"sourceManifestHash} objects. Required: a locked platform service binds the " +
					"repository, commit and manifest hash a deployment builds from, which " +
					"fleet-control cannot derive — it holds no platform-service index. A declared " +
					"service with no identity here is refused rather than locked against a guess.",
			},
			"lock": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "The resolved DistributionLock, as a JSON object (biomes[], platformServices[], …).",
			},
		},
	}
}

func (d *distributionLockDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			"Expected *client.Client. This is a bug in the provider; please report it.")
		return
	}
	d.client = c
}

func (d *distributionLockDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data distributionLockModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	distribution, err := normalizedToValue(data.Distribution)
	if err != nil {
		resp.Diagnostics.AddError("Invalid distribution", err.Error())
		return
	}
	biomesValue, err := normalizedToValue(data.AvailableBiomes)
	if err != nil {
		resp.Diagnostics.AddError("Invalid available_biomes", err.Error())
		return
	}
	biomes, ok := biomesValue.([]any)
	if !ok {
		resp.Diagnostics.AddError("Invalid available_biomes",
			"available_biomes must be a JSON array of AvailableBiome objects.")
		return
	}
	ownersValue, err := normalizedToValue(data.CapabilityDomainOwners)
	if err != nil {
		resp.Diagnostics.AddError("Invalid capability_domain_owners", err.Error())
		return
	}
	owners, ok := ownersValue.([]any)
	if !ok {
		resp.Diagnostics.AddError("Invalid capability_domain_owners",
			"capability_domain_owners must be a JSON array of {domain, biomeId} objects.")
		return
	}
	// Refused here rather than at the wire: an empty table is indistinguishable
	// from "no domain is first-party", which is fail-OPEN — every platform
	// capability ref would become bindable by an org admin.
	if len(owners) == 0 {
		resp.Diagnostics.AddError("Invalid capability_domain_owners",
			"capability_domain_owners must not be empty. An empty reservation table means "+
				"\"no domain is first-party\", which would let an org admin bind a platform "+
				"capability ref to a URL they control.")
		return
	}
	sourcesValue, err := normalizedToValue(data.PlatformServiceSources)
	if err != nil {
		resp.Diagnostics.AddError("Invalid platform_service_sources", err.Error())
		return
	}
	sources, ok := sourcesValue.([]any)
	if !ok {
		resp.Diagnostics.AddError("Invalid platform_service_sources",
			"platform_service_sources must be a JSON array of PlatformServiceSourceIdentity objects.")
		return
	}

	lock, err := d.client.ResolveDistributionLock(ctx, distribution, biomes, owners, sources)
	if err != nil {
		resp.Diagnostics.AddError("Failed to resolve distribution lock", err.Error())
		return
	}

	normalizedLock, err := normalizedFromValue(lock)
	if err != nil {
		resp.Diagnostics.AddError("Failed to encode distribution lock", err.Error())
		return
	}
	data.Lock = normalizedLock

	if id, ok := lock["distributionId"].(string); ok && id != "" {
		data.ID = types.StringValue(id)
	} else {
		// Fall back to the input hash, then a constant, so id is always known.
		if h, ok := lock["inputHash"].(string); ok && h != "" {
			data.ID = types.StringValue(h)
		} else {
			data.ID = types.StringValue("distribution-lock")
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
