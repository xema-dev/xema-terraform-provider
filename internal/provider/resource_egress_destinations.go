// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// egressDestinationsKind is the XemaResourceKind wire value of the
// installation's egress destination policy.
const egressDestinationsKind = "egress-destinations"

// egressDestinationsID is the policy's one physical id.
const egressDestinationsID = "installation"

// egressRevisionConflictCode is the control plane's code for a write that lost
// a compare-and-set against another writer.
const egressRevisionConflictCode = "EGRESS_DESTINATIONS_REVISION_CONFLICT"

var (
	_ resource.Resource                = (*egressDestinationsResource)(nil)
	_ resource.ResourceWithConfigure   = (*egressDestinationsResource)(nil)
	_ resource.ResourceWithImportState = (*egressDestinationsResource)(nil)
)

type egressDestinationsResource struct {
	client *client.Client
}

// NewEgressDestinationsResource is the factory registered with the provider.
func NewEgressDestinationsResource() resource.Resource {
	return &egressDestinationsResource{}
}

// egressDestinationsModel mirrors the egress-destinations kind: the policy
// `{ kind, grants }` plus the read-only status. The revision the server-side
// adapter reads and sends is reported, never configurable.
type egressDestinationsModel struct {
	ID         types.String         `tfsdk:"id"`
	Kind       types.String         `tfsdk:"kind"`
	Grants     jsontypes.Normalized `tfsdk:"grants"`
	Revision   types.Int64          `tfsdk:"revision"`
	Configured types.Bool           `tfsdk:"configured"`
}

func (r *egressDestinationsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_egress_destinations"
}

func (r *egressDestinationsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The installation's egress destination policy: which private-use destinations the " +
			"platform may fetch from beyond the public internet. One per installation (id " +
			"`installation`), written only by the installation owner (anyone else is refused with 403). " +
			"The declared policy replaces the whole policy; an unchanged policy writes nothing. " +
			"Destroying the resource resets the policy to `public-only`, which only narrows egress. " +
			"Import with the id `installation`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Always `installation`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"kind": schema.StringAttribute{
				Required:    true,
				Description: "The policy kind, as biome-host defines it (for example `public-only`).",
			},
			"grants": schema.StringAttribute{
				Optional:   true,
				CustomType: jsontypes.NormalizedType{},
				Description: "The policy's grants, a JSON list passed through to biome-host unchanged " +
					"(write it with `jsonencode`). biome-host validates the policy whole and names " +
					"every problem on refusal.",
			},
			"revision": schema.Int64Attribute{
				Computed: true,
				Description: "Read-only: the policy's revision, advanced by each write. The server reads " +
					"and sends it on every write; it is not configurable.",
			},
			"configured": schema.BoolAttribute{
				Computed:    true,
				Description: "Read-only: whether the policy has ever been written.",
			},
		},
	}
}

func (r *egressDestinationsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (m egressDestinationsModel) toSpec() (map[string]any, error) {
	spec := map[string]any{"kind": m.Kind.ValueString()}
	grants, err := normalizedToValue(m.Grants)
	if err != nil {
		return nil, fmt.Errorf("`grants`: %w", err)
	}
	if grants != nil {
		if _, ok := grants.([]any); !ok {
			return nil, fmt.Errorf("`grants` must be a JSON list")
		}
		spec["grants"] = grants
	}
	return spec, nil
}

func (m *egressDestinationsModel) applyReadback(res *client.Resource) error {
	m.ID = types.StringValue(egressDestinationsID)
	m.Kind = types.StringValue(specString(res.Spec, "kind"))
	m.Grants = specNormalized(res.Spec, "grants")
	revision, ok := res.Status["revision"].(float64)
	if !ok {
		return fmt.Errorf("egress-destinations read-back has no status.revision")
	}
	configured, ok := res.Status["configured"].(bool)
	if !ok {
		return fmt.Errorf("egress-destinations read-back has no status.configured")
	}
	m.Revision = types.Int64Value(int64(revision))
	m.Configured = types.BoolValue(configured)
	return nil
}

// addEgressError turns a revision conflict into a clear diagnostic and leaves
// every other refusal as the control plane worded it.
func addEgressError(diags *diag.Diagnostics, summary string, err error) {
	if client.AsAPIError(err, egressRevisionConflictCode) != nil {
		diags.AddError("Egress destinations changed during apply",
			"Another writer changed the installation's egress destinations while this apply was writing "+
				"them, so nothing was written (EGRESS_DESTINATIONS_REVISION_CONFLICT). Run `terraform "+
				"refresh` or plan again to re-read the policy, then apply again. "+err.Error())
		return
	}
	diags.AddError(summary, err.Error())
}

// write replaces the policy (create and update are the same replace upstream,
// at the revision the server reads) and stores the read-back.
func (r *egressDestinationsResource) write(ctx context.Context, plan *egressDestinationsModel, diags *diag.Diagnostics) bool {
	spec, err := plan.toSpec()
	if err != nil {
		diags.AddError("Invalid egress destinations policy", err.Error())
		return false
	}
	handle, err := r.client.Create(ctx, egressDestinationsKind, spec)
	if err != nil {
		addEgressError(diags, "Failed to write egress destinations", err)
		return false
	}
	res, err := r.client.Read(ctx, egressDestinationsKind, handle.PhysicalID)
	if err != nil {
		diags.AddError("Failed to read egress destinations after write", err.Error())
		return false
	}
	declared := plan.Grants
	if err := plan.applyReadback(res); err != nil {
		diags.AddError("Unexpected egress destinations read-back", err.Error())
		return false
	}
	if eq, _ := declared.StringSemanticEquals(ctx, plan.Grants); eq {
		plan.Grants = declared
	}
	return true
}

func (r *egressDestinationsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan egressDestinationsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.write(ctx, &plan, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

func (r *egressDestinationsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state egressDestinationsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.client.Read(ctx, egressDestinationsKind, egressDestinationsID)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read egress destinations", err.Error())
		return
	}
	declared := state.Grants
	if err := state.applyReadback(res); err != nil {
		resp.Diagnostics.AddError("Unexpected egress destinations read-back", err.Error())
		return
	}
	if eq, _ := declared.StringSemanticEquals(ctx, state.Grants); eq {
		state.Grants = declared
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *egressDestinationsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan egressDestinationsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.write(ctx, &plan, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

// Delete resets the policy to public-only (the server does this; there is no
// upstream DELETE). A revision conflict surfaces as a clear diagnostic.
func (r *egressDestinationsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if err := r.client.Delete(ctx, egressDestinationsKind, egressDestinationsID); err != nil && !client.IsNotFound(err) {
		addEgressError(&resp.Diagnostics, "Failed to reset egress destinations", err)
	}
}

func (r *egressDestinationsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != egressDestinationsID {
		resp.Diagnostics.AddError("Invalid import id",
			fmt.Sprintf("the installation has one egress destination policy; import it with id %q, got %q.", egressDestinationsID, req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
