// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// orgSettingKind is the XemaResourceKind wire value of an organisation setting.
const orgSettingKind = "org-setting"

var (
	_ resource.Resource                = (*orgSettingResource)(nil)
	_ resource.ResourceWithConfigure   = (*orgSettingResource)(nil)
	_ resource.ResourceWithImportState = (*orgSettingResource)(nil)
)

type orgSettingResource struct {
	client *client.Client
}

// NewOrgSettingResource is the factory registered with the provider.
func NewOrgSettingResource() resource.Resource {
	return &orgSettingResource{}
}

// orgSettingModel mirrors the org-setting kind: the admin value of one setting
// at the organisation's own scope. The id is the setting key.
type orgSettingModel struct {
	ID       types.String         `tfsdk:"id"`
	Key      types.String         `tfsdk:"key"`
	Value    jsontypes.Normalized `tfsdk:"value"`
	Revision types.Int64          `tfsdk:"revision"`
}

func (r *orgSettingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_setting"
}

func (r *orgSettingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The organisation's own (admin) value of one platform setting, managed through " +
			"space-registry via the control plane. The setting is named by its key within the org. " +
			"Destroying the resource clears the value, so the setting reverts to its declaration or " +
			"default. A setting the deployment declares refuses the write (403). Import with the " +
			"setting key.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The setting key (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "The setting key, `<namespace>:<name>@<major>`. A changed key replaces the resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				Required:   true,
				CustomType: jsontypes.NormalizedType{},
				Description: "The value in wire form, as a JSON scalar: a boolean, a number or a string " +
					"(a duration is an ISO-8601 string). Write it with `jsonencode`, for example " +
					"`jsonencode(true)` or `jsonencode(\"PT5M\")`.",
			},
			"revision": schema.Int64Attribute{
				Computed:    true,
				Description: "Read-only: the setting's revision, advanced by each write.",
			},
		},
	}
}

func (r *orgSettingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

// toSpec builds the spec, refusing a value that is not a JSON scalar before any call.
func (m orgSettingModel) toSpec() (map[string]any, error) {
	value, err := normalizedToValue(m.Value)
	if err != nil {
		return nil, err
	}
	switch value.(type) {
	case bool, string, float64:
	default:
		return nil, fmt.Errorf("`value` must be a JSON boolean, number or string")
	}
	return map[string]any{"key": m.Key.ValueString(), "value": value}, nil
}

func (m *orgSettingModel) applyReadback(res *client.Resource) {
	m.ID = types.StringValue(res.PhysicalID)
	m.Key = types.StringValue(specString(res.Spec, "key"))
	if m.Key.ValueString() == "" {
		m.Key = types.StringValue(res.PhysicalID)
	}
	if raw, ok := res.Spec["value"]; ok && raw != nil {
		if buf, err := json.Marshal(raw); err == nil {
			m.Value = jsontypes.NewNormalizedValue(string(buf))
		}
	}
	if rev, ok := res.Status["revision"].(float64); ok {
		m.Revision = types.Int64Value(int64(rev))
	} else {
		m.Revision = types.Int64Null()
	}
}

// write states the value (create and update are the same PUT upstream) and
// stores the read-back.
func (r *orgSettingResource) write(ctx context.Context, plan *orgSettingModel, update bool, diags interface {
	AddError(summary, detail string)
}) bool {
	spec, err := plan.toSpec()
	if err != nil {
		diags.AddError("Invalid org setting", err.Error())
		return false
	}
	id := plan.Key.ValueString()
	if update {
		delete(spec, "key")
		if _, err := r.client.Update(ctx, orgSettingKind, id, spec); err != nil {
			diags.AddError("Failed to update org setting", err.Error())
			return false
		}
	} else {
		handle, err := r.client.Create(ctx, orgSettingKind, spec)
		if err != nil {
			diags.AddError("Failed to create org setting", err.Error())
			return false
		}
		id = handle.PhysicalID
	}
	res, err := r.client.Read(ctx, orgSettingKind, id)
	if err != nil {
		diags.AddError("Failed to read org setting after write", err.Error())
		return false
	}
	declared := plan.Value
	plan.applyReadback(res)
	// Keep the author's spelling when the server value is semantically equal.
	if eq, _ := declared.StringSemanticEquals(context.Background(), plan.Value); eq {
		plan.Value = declared
	}
	return true
}

func (r *orgSettingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgSettingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.write(ctx, &plan, false, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

func (r *orgSettingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgSettingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.client.Read(ctx, orgSettingKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read org setting", err.Error())
		return
	}
	declared := state.Value
	state.applyReadback(res)
	if eq, _ := declared.StringSemanticEquals(ctx, state.Value); eq {
		state.Value = declared
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgSettingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan orgSettingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.write(ctx, &plan, true, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

// Delete clears the organisation's value; the setting reverts to its declaration or default.
func (r *orgSettingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgSettingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, orgSettingKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to clear org setting", err.Error())
	}
}

func (r *orgSettingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
