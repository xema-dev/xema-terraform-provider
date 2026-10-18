// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// modelStrategyKind is the XemaResourceKind wire value for model strategies.
const modelStrategyKind = "model-strategy"

var (
	_ resource.Resource                = (*modelStrategyResource)(nil)
	_ resource.ResourceWithConfigure   = (*modelStrategyResource)(nil)
	_ resource.ResourceWithImportState = (*modelStrategyResource)(nil)
)

type modelStrategyResource struct {
	client *client.Client
}

// NewModelStrategyResource is the factory registered with the provider.
func NewModelStrategyResource() resource.Resource {
	return &modelStrategyResource{}
}

type modelStrategyModel struct {
	ID          types.String    `tfsdk:"id"`
	Slug        types.String    `tfsdk:"slug"`
	DisplayName types.String    `tfsdk:"display_name"`
	Description types.String    `tfsdk:"description"`
	Rules       []ruleBodyModel `tfsdk:"rules"`
}

func (r *modelStrategyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model_strategy"
}

func (r *modelStrategyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// `description` is Optional AND Computed: the XEMA API is authoritative for
	// current state, and a declaration that omits it must be able to read back
	// the service's value (null or set) without Terraform refusing its own
	// result with "Provider produced inconsistent result after apply".
	resp.Schema = schema.Schema{
		Description: "A model strategy: a named container of model resolution rules, bound to projects. " +
			"It holds only rules, declared in `rules`; each rule targets a model id. The rule set is replaced " +
			"as a whole: the declared `rules` set is authoritative and replaces the strategy's rules on apply, " +
			"including a change made since Terraform's refresh. Only the control plane's own read-write window " +
			"is guarded by the strategy's version. " +
			"Managed through llm-registry-api via the control plane. The API is authoritative: every " +
			"server-owned value is refreshed from it on each read, so state can be rebuilt by import.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted strategy id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"slug": schema.StringAttribute{
				Required: true,
				Description: "Stable slug, unique within the organization. It is the managed key, " +
					"and it is immutable upstream — changing it declares a different strategy.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable strategy name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "What this strategy is for.",
			},
			"rules": schema.SetNestedAttribute{
				Required: true,
				Description: "The strategy's complete rule set (`[]` for none). Each rule has the same body as " +
					"`xema_model_resolution_rule` and competes with the organization's rules for the projects " +
					"the strategy is bound to; a strategy rule wins a tie.",
				NestedObject: schema.NestedAttributeObject{Attributes: strategyRuleAttributes()},
			},
		},
	}
}

func (r *modelStrategyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

// strategyRuleAttributes is a rule body as a strategy declares it. Priority is
// required here: a set element cannot read back a server default the plan did
// not contain.
func strategyRuleAttributes() map[string]schema.Attribute {
	attrs := ruleTargetAttributes()
	attrs["selector"] = ruleSelectorAttribute()
	attrs["priority"] = schema.Int64Attribute{Required: true, Description: rulePriorityDescription}
	return attrs
}

func (m modelStrategyModel) toSpec(ctx context.Context) (map[string]any, error) {
	rules := make([]any, 0, len(m.Rules))
	for _, rule := range m.Rules {
		spec, err := rule.toSpec(ctx)
		if err != nil {
			return nil, err
		}
		rules = append(rules, spec)
	}
	spec := map[string]any{
		"slug":        m.Slug.ValueString(),
		"displayName": m.DisplayName.ValueString(),
		"rules":       rules,
	}
	if s := optString(m.Description); s != "" {
		spec["description"] = s
	}
	return spec, nil
}

func (m *modelStrategyModel) applyReadback(ctx context.Context, spec map[string]any) error {
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.DisplayName = types.StringValue(specString(spec, "displayName"))
	m.Description = strOrNull(specString(spec, "description"))

	// An empty slice, not nil: nil reads back as a NULL set, and an empty rule
	// set is the declared `rules = []`.
	m.Rules = []ruleBodyModel{}
	raw, _ := spec["rules"].([]any)
	for _, item := range raw {
		ruleSpec, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("strategy rule is not an object: %#v", item)
		}
		var rule ruleBodyModel
		if err := rule.applyReadback(ctx, ruleSpec); err != nil {
			return err
		}
		m.Rules = append(m.Rules, rule)
	}
	return nil
}

func (r *modelStrategyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modelStrategyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid strategy rules", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, modelStrategyKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create model strategy", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, modelStrategyKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read model strategy after create", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map model strategy", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelStrategyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state modelStrategyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, modelStrategyKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read model strategy", err.Error())
		return
	}
	if err := state.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map model strategy", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *modelStrategyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan modelStrategyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid strategy rules", err.Error())
		return
	}
	res, err := r.client.Update(ctx, modelStrategyKind, plan.ID.ValueString(), spec)
	if err != nil {
		if client.IsConflict(err) {
			resp.Diagnostics.AddError("Model strategy rules changed concurrently",
				"The strategy's rule set changed between the control plane's read of its version and its replace, "+
					"so that replace was refused. Run the apply again to converge on the declared rules. "+
					"Upstream said: "+err.Error())
			return
		}
		resp.Diagnostics.AddError("Failed to update model strategy", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map model strategy", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelStrategyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state modelStrategyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, modelStrategyKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete model strategy", err.Error())
	}
}

func (r *modelStrategyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
