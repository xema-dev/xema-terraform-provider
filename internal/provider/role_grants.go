// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// roleGrantModel is one capability a role holds, as the role kind's `grants`
// spec carries it: { capability, environment, requiresApproval?, constraints?,
// where? }. (capability, environment) is the grant's natural key.
type roleGrantModel struct {
	Capability       types.String         `tfsdk:"capability"`
	Environment      types.String         `tfsdk:"environment"`
	RequiresApproval types.Bool           `tfsdk:"requires_approval"`
	Constraints      jsontypes.Normalized `tfsdk:"constraints"`
	Where            jsontypes.Normalized `tfsdk:"where"`
}

const roleGrantsDescription = "The role's complete grant set. The control plane reconciles it both ways: a " +
	"grant the role holds that is not declared here is removed. Leaving `grants` unset declares the " +
	"empty set, so it withdraws every grant the role holds; it never means \"leave the grants alone\"."

const roleGrantCapabilityDescription = "Canonical capability ref, e.g. `authorization:installation-consent.decide@1`."

const roleGrantEnvironmentDescription = "Built-in environment slug the grant applies in, e.g. `org` or `project`."

const roleGrantApprovalDescription = "Whether using the grant needs an approval. The service may raise it to " +
	"true on its own (its delegation ceiling), and a declared false or unset over a live true is not a " +
	"difference; to lower a live gate, remove the grant and add it back."

const roleGrantConstraintsDescription = "Optional constraints as a JSON object " +
	"(`maxCallsPerHour`, `maxCostUsd`, `rateLimit`)."

const roleGrantWhereDescription = "Optional scope condition as a JSON object; unset for an unscoped grant."

func roleGrantAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"capability":        schema.StringAttribute{Required: true, Description: roleGrantCapabilityDescription},
		"environment":       schema.StringAttribute{Required: true, Description: roleGrantEnvironmentDescription},
		"requires_approval": schema.BoolAttribute{Optional: true, Description: roleGrantApprovalDescription},
		"constraints": schema.StringAttribute{
			Optional:    true,
			CustomType:  jsontypes.NormalizedType{},
			Description: roleGrantConstraintsDescription,
		},
		"where": schema.StringAttribute{
			Optional:    true,
			CustomType:  jsontypes.NormalizedType{},
			Description: roleGrantWhereDescription,
		},
	}
}

func roleGrantDataSourceAttributes() map[string]dsschema.Attribute {
	return map[string]dsschema.Attribute{
		"capability":        dsschema.StringAttribute{Computed: true, Description: roleGrantCapabilityDescription},
		"environment":       dsschema.StringAttribute{Computed: true, Description: roleGrantEnvironmentDescription},
		"requires_approval": dsschema.BoolAttribute{Computed: true, Description: roleGrantApprovalDescription},
		"constraints": dsschema.StringAttribute{
			Computed:    true,
			CustomType:  jsontypes.NormalizedType{},
			Description: roleGrantConstraintsDescription,
		},
		"where": dsschema.StringAttribute{
			Computed:    true,
			CustomType:  jsontypes.NormalizedType{},
			Description: roleGrantWhereDescription,
		},
	}
}

func (g roleGrantModel) key() string {
	return g.Capability.ValueString() + "\x00" + g.Environment.ValueString()
}

// roleGrantsToSpec is the declared grant set as the spec's `grants`. It is
// never nil: an unset `grants` is sent as the empty set, so the role kind
// reconciles to what the configuration says rather than to an omission.
func roleGrantsToSpec(grants []roleGrantModel) ([]any, error) {
	out := make([]any, 0, len(grants))
	for _, g := range grants {
		spec := map[string]any{
			"capability":  g.Capability.ValueString(),
			"environment": g.Environment.ValueString(),
		}
		if !g.RequiresApproval.IsNull() && !g.RequiresApproval.IsUnknown() {
			spec["requiresApproval"] = g.RequiresApproval.ValueBool()
		}
		constraints, err := normalizedToValue(g.Constraints)
		if err != nil {
			return nil, fmt.Errorf("grant %s constraints: %w", g.Capability.ValueString(), err)
		}
		if constraints != nil {
			spec["constraints"] = constraints
		}
		where, err := normalizedToValue(g.Where)
		if err != nil {
			return nil, fmt.Errorf("grant %s where: %w", g.Capability.ValueString(), err)
		}
		if where != nil {
			spec["where"] = where
		}
		out = append(out, spec)
	}
	return out, nil
}

// roleGrantsFromSpec reads the server's grants into the attribute, keeping the
// prior value (the plan on apply, the state on refresh) wherever the server's
// value means the same, so an applied declaration reads back exactly as
// declared and a refresh shows only real differences. An empty grant set reads
// as unset, unless the prior value was the declared empty set. Constraints and
// where keep the prior JSON text when it is equal as JSON. requires_approval
// keeps a prior false or unset over a live true, the role kind's own
// one-direction comparison; with no prior grant (import) a live false reads as
// unset, the default.
func roleGrantsFromSpec(spec map[string]any, prior []roleGrantModel) []roleGrantModel {
	raw, _ := spec["grants"].([]any)
	if len(raw) == 0 {
		if prior != nil && len(prior) == 0 {
			return []roleGrantModel{}
		}
		return nil
	}
	priorByKey := make(map[string]roleGrantModel, len(prior))
	for _, g := range prior {
		priorByKey[g.key()] = g
	}
	out := make([]roleGrantModel, 0, len(raw))
	for _, item := range raw {
		live, ok := item.(map[string]any)
		if !ok {
			continue
		}
		g := roleGrantModel{
			Capability:  types.StringValue(specString(live, "capability")),
			Environment: types.StringValue(specString(live, "environment")),
		}
		before, hadPrior := priorByKey[g.key()]
		liveApproval, _ := live["requiresApproval"].(bool)
		switch {
		case hadPrior && !before.RequiresApproval.ValueBool():
			// A prior false or unset over either live value: not a difference.
			g.RequiresApproval = before.RequiresApproval
		case liveApproval:
			g.RequiresApproval = types.BoolValue(true)
		case hadPrior:
			// Declared true, live false: a real difference.
			g.RequiresApproval = types.BoolValue(false)
		default:
			g.RequiresApproval = types.BoolNull()
		}
		g.Constraints = normalizedKeepingPrior(live, "constraints", before.Constraints, hadPrior)
		g.Where = normalizedKeepingPrior(live, "where", before.Where, hadPrior)
		out = append(out, g)
	}
	return out
}

// normalizedKeepingPrior is the server's JSON field, or the prior text when it
// is the same JSON value.
func normalizedKeepingPrior(live map[string]any, key string, prior jsontypes.Normalized, hadPrior bool) jsontypes.Normalized {
	value := specNormalized(live, key)
	if !hadPrior || prior.IsNull() || prior.IsUnknown() || value.IsNull() {
		return value
	}
	var a, b any
	if json.Unmarshal([]byte(prior.ValueString()), &a) != nil || json.Unmarshal([]byte(value.ValueString()), &b) != nil {
		return value
	}
	if reflect.DeepEqual(a, b) {
		return prior
	}
	return value
}
