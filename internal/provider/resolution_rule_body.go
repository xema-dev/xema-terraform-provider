// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ruleBodyModel is the body of one Model Resolution Matrix rule: what it matches
// and which model it routes to. It is the whole rule for a strategy's nested
// `rules` and the rule minus its id for `xema_model_resolution_rule`, so the
// spec mapping and the schema attributes live here once.
type ruleBodyModel struct {
	Selector              *selectorModel `tfsdk:"selector"`
	TargetModelID         types.String   `tfsdk:"target_model_id"`
	TargetTemperature     types.Float64  `tfsdk:"target_temperature"`
	TargetReasoningEffort types.String   `tfsdk:"target_reasoning_effort"`
	Priority              types.Int64    `tfsdk:"priority"`
}

func (m ruleBodyModel) toSpec(ctx context.Context) (map[string]any, error) {
	spec := map[string]any{"targetModelId": m.TargetModelID.ValueString()}

	if m.Selector != nil {
		sel := map[string]any{}
		if v := optString(m.Selector.Agent); v != "" {
			sel["agent"] = v
		}
		if v := optString(m.Selector.Skill); v != "" {
			sel["skill"] = v
		}
		if v := optString(m.Selector.Project); v != "" {
			sel["project"] = v
		}
		if v := optString(m.Selector.Stage); v != "" {
			sel["stage"] = v
		}
		if v := optString(m.Selector.Purpose); v != "" {
			sel["purpose"] = v
		}
		if v := optString(m.Selector.ModelLane); v != "" {
			sel["modelLane"] = v
		}
		if !m.Selector.Extra.IsNull() && !m.Selector.Extra.IsUnknown() {
			extra := map[string]string{}
			if diags := m.Selector.Extra.ElementsAs(ctx, &extra, false); diags.HasError() {
				return nil, errFromDiags(diags)
			}
			if len(extra) > 0 {
				sel["extra"] = extra
			}
		}
		spec["selector"] = sel
	}

	if v := optString(m.TargetReasoningEffort); v != "" {
		spec["targetReasoningEffort"] = v
	}
	if !m.TargetTemperature.IsNull() && !m.TargetTemperature.IsUnknown() {
		spec["targetTemperature"] = m.TargetTemperature.ValueFloat64()
	}
	if !m.Priority.IsNull() && !m.Priority.IsUnknown() {
		spec["priority"] = m.Priority.ValueInt64()
	}
	return spec, nil
}

func (m *ruleBodyModel) applyReadback(ctx context.Context, spec map[string]any) error {
	m.TargetModelID = types.StringValue(specString(spec, "targetModelId"))
	m.TargetReasoningEffort = strOrNull(specString(spec, "targetReasoningEffort"))

	if v, ok := numberFromSpec(spec, "targetTemperature"); ok {
		m.TargetTemperature = types.Float64Value(v)
	} else {
		m.TargetTemperature = types.Float64Null()
	}
	if v, ok := numberFromSpec(spec, "priority"); ok {
		m.Priority = types.Int64Value(int64(v))
	} else {
		m.Priority = types.Int64Null()
	}

	rawSel, ok := spec["selector"].(map[string]any)
	if !ok || len(rawSel) == 0 {
		m.Selector = nil
		return nil
	}
	sel := &selectorModel{
		Agent:     strOrNull(specString(rawSel, "agent")),
		Skill:     strOrNull(specString(rawSel, "skill")),
		Project:   strOrNull(specString(rawSel, "project")),
		Stage:     strOrNull(specString(rawSel, "stage")),
		Purpose:   strOrNull(specString(rawSel, "purpose")),
		ModelLane: strOrNull(specString(rawSel, "modelLane")),
		Extra:     types.MapNull(types.StringType),
	}
	if rawExtra, ok := rawSel["extra"].(map[string]any); ok && len(rawExtra) > 0 {
		elems := map[string]string{}
		for k, v := range rawExtra {
			if s, ok := v.(string); ok {
				elems[k] = s
			}
		}
		mv, diags := types.MapValueFrom(ctx, types.StringType, elems)
		if diags.HasError() {
			return errFromDiags(diags)
		}
		sel.Extra = mv
	}
	m.Selector = sel
	return nil
}

// ruleSelectorAttribute is the selector block shared by the rule resource and a
// strategy's nested rules.
func ruleSelectorAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		Required:    true,
		Description: "Dimensional selector. At least one dimension is required: an empty selector is refused. The organization default model is `xema_org_model_default`.",
		Attributes: map[string]schema.Attribute{
			"agent":      schema.StringAttribute{Optional: true, Description: "Match on agent slug."},
			"skill":      schema.StringAttribute{Optional: true, Description: "Match on skill slug."},
			"project":    schema.StringAttribute{Optional: true, Description: "Match on project id."},
			"stage":      schema.StringAttribute{Optional: true, Description: "Match on pipeline stage/phase key."},
			"purpose":    schema.StringAttribute{Optional: true, Description: "Match on invocation purpose."},
			"model_lane": schema.StringAttribute{Optional: true, Description: modelLaneSelectorDescription},
			"extra": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Any registry-added selector dimension, as a string map.",
			},
		},
	}
}

// ruleTargetAttributes are the routing attributes shared by the rule resource and
// a strategy's nested rules (everything but selector and priority).
func ruleTargetAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"target_model_id": schema.StringAttribute{
			Required: true,
			Description: "Registry model record id (`LlmModel.id`) the rule routes to. It must be an active " +
				"model on an active provider the organization or the platform owns; otherwise the write is refused.",
		},
		"target_temperature": schema.Float64Attribute{
			Optional:    true,
			Description: "Sampling temperature (0-2) carried by the resolved model.",
		},
		"target_reasoning_effort": schema.StringAttribute{
			Optional: true,
			Description: "Reasoning effort carried by the resolved model, forwarded verbatim to the provider. " +
				"OPEN vocabulary owned by the provider.",
		},
	}
}

const rulePriorityDescription = "Tie-breaker priority when two rules match the same number of dimensions."

// ruleDataSourceAttributes is the computed view of a rule body for the data
// sources that expose one (a rule, and a strategy's rules).
func ruleDataSourceAttributes() map[string]dsschema.Attribute {
	return map[string]dsschema.Attribute{
		"selector": dsschema.SingleNestedAttribute{
			Computed:    true,
			Description: "Dimensional selector (at least one dimension).",
			Attributes: map[string]dsschema.Attribute{
				"agent":      dsschema.StringAttribute{Computed: true, Description: "Match on agent slug."},
				"skill":      dsschema.StringAttribute{Computed: true, Description: "Match on skill slug."},
				"project":    dsschema.StringAttribute{Computed: true, Description: "Match on project id."},
				"stage":      dsschema.StringAttribute{Computed: true, Description: "Match on pipeline stage/phase key."},
				"purpose":    dsschema.StringAttribute{Computed: true, Description: "Match on invocation purpose."},
				"model_lane": dsschema.StringAttribute{Computed: true, Description: modelLaneSelectorDescription},
				"extra": dsschema.MapAttribute{
					Computed:    true,
					ElementType: types.StringType,
					Description: "Any registry-added selector dimension, as a string map.",
				},
			},
		},
		"target_model_id": dsschema.StringAttribute{
			Computed:    true,
			Description: "Registry model record id (`LlmModel.id`) the rule routes to.",
		},
		"target_temperature": dsschema.Float64Attribute{
			Computed:    true,
			Description: "Sampling temperature carried by the resolved model.",
		},
		"target_reasoning_effort": dsschema.StringAttribute{
			Computed:    true,
			Description: "Reasoning effort carried by the resolved model.",
		},
		"priority": dsschema.Int64Attribute{
			Computed:    true,
			Description: rulePriorityDescription,
		},
	}
}
