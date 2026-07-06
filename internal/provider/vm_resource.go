package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hllme/terraform-provider-idcloudhost/internal/client"
)

const (
	vmStatusRunning = "running"
	vmStatusStopped = "stopped"

	defaultCreateTimeout = 10 * time.Minute
	defaultDeleteTimeout = 10 * time.Minute
)

// pollInterval is a var (not const) so tests can shrink it instead of
// waiting on the real 5s cadence.
var pollInterval = 5 * time.Second

// Ensure vmResource satisfies the expected interfaces.
var (
	_ resource.Resource                = &vmResource{}
	_ resource.ResourceWithConfigure   = &vmResource{}
	_ resource.ResourceWithImportState = &vmResource{}
)

func NewVMResource() resource.Resource {
	return &vmResource{}
}

type vmResource struct {
	client          *client.Client
	defaultLocation string
}

// vmResourceModel maps the idcloudhost_vm schema to Go types, per
// PROVIDER_DESIGN.md §3.1.
type vmResourceModel struct {
	UUID               types.String   `tfsdk:"uuid"`
	Name               types.String   `tfsdk:"name"`
	BillingAccountID   types.Int64    `tfsdk:"billing_account_id"`
	Location           types.String   `tfsdk:"location"`
	OSName             types.String   `tfsdk:"os_name"`
	OSVersion          types.String   `tfsdk:"os_version"`
	VCPU               types.Int64    `tfsdk:"vcpu"`
	RAM                types.Int64    `tfsdk:"ram"`
	Disks              types.Int64    `tfsdk:"disks"`
	Username           types.String   `tfsdk:"username"`
	Password           types.String   `tfsdk:"password"`
	PublicKey          types.String   `tfsdk:"public_key"`
	CloudInit          types.String   `tfsdk:"cloud_init"`
	PrivateNetworkUUID types.String   `tfsdk:"private_network_uuid"`
	FloatIPAddress     types.String   `tfsdk:"float_ip_address"`
	ReservePublicIP    types.Bool     `tfsdk:"reserve_public_ip"`
	DesiredStatus      types.String   `tfsdk:"desired_status"`
	Backup             types.Bool     `tfsdk:"backup"`
	Timeouts           timeouts.Value `tfsdk:"timeouts"`
}

func (r *vmResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm"
}

func (r *vmResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An IDCloudHost virtual machine, with first-class cloud-init user-data support and async create/destroy lifecycle handling.",
		Attributes: map[string]schema.Attribute{
			"uuid": schema.StringAttribute{
				Computed:      true,
				Description:   "The VM's unique identifier. Used as the resource ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The VM's display name.",
			},
			"billing_account_id": schema.Int64Attribute{
				Required:      true,
				Description:   "The billing account this VM is billed to.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"location": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Datacenter location (jkt01, jkt02, jkt03, sgp01). Defaults to the provider's default_location.",
				Validators: []validator.String{
					stringvalidator.OneOf("jkt01", "jkt02", "jkt03", "sgp01"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"os_name": schema.StringAttribute{
				Required:      true,
				Description:   "Operating system family, e.g. ubuntu.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"os_version": schema.StringAttribute{
				Required:      true,
				Description:   "Operating system version. Validate against the live /v1/config/vm_images endpoint, not the stale parameters enum.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vcpu": schema.Int64Attribute{
				Required:    true,
				Description: "Number of vCPU cores (API range 1-16). Changing this requires the VM to be stopped.",
			},
			"ram": schema.Int64Attribute{
				Required:    true,
				Description: "RAM in MB (API range 512-65536). Changing this requires the VM to be stopped.",
			},
			"disks": schema.Int64Attribute{
				Required:      true,
				Description:   "Primary disk size in GB. ForceNew in v1; disk grow is out of scope.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"username": schema.StringAttribute{
				Required:      true,
				Description:   "Initial OS user created on the VM.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"password": schema.StringAttribute{
				Required:      true,
				Sensitive:     true,
				Description:   "Initial OS user password. Must be >= 8 chars with upper, lower, and digit.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"public_key": schema.StringAttribute{
				Optional:      true,
				Description:   "Optional SSH public key. Break-glass only; the project's default is no SSH.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"cloud_init": schema.StringAttribute{
				Optional:      true,
				Description:   "cloud-init user-data (YAML or JSON), merged over platform defaults by the API. A user-provided `users:` key overrides the username/password injection above.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"private_network_uuid": schema.StringAttribute{
				Optional:      true,
				Description:   "Private network to bind this VM to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"float_ip_address": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Floating IP address assigned to this VM.",
			},
			"reserve_public_ip": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the API should reserve a public IP automatically. Typically false when using an explicit floating IP.",
			},
			"desired_status": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Desired VM power state: running or stopped.",
				Validators: []validator.String{
					stringvalidator.OneOf(vmStatusRunning, vmStatusStopped),
				},
			},
			"backup": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Platform backup flag. The project's DR pillar owns backups separately, so this defaults to false.",
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create: true,
				Delete: true,
			}),
		},
	}
}

func (r *vmResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	data, ok := req.ProviderData.(*ProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *provider.ProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = data.Client
	r.defaultLocation = data.DefaultLocation
}

func (r *vmResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("uuid"), req, resp)
}

func (r *vmResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	location := plan.Location.ValueString()
	if location == "" {
		location = r.defaultLocation
	}

	desiredStatus := plan.DesiredStatus.ValueString()
	if desiredStatus == "" {
		desiredStatus = vmStatusRunning
	}

	input := client.CreateVMInput{
		Name:               plan.Name.ValueString(),
		BillingAccountID:   int(plan.BillingAccountID.ValueInt64()),
		Location:           location,
		OSName:             plan.OSName.ValueString(),
		OSVersion:          plan.OSVersion.ValueString(),
		VCPU:               int(plan.VCPU.ValueInt64()),
		RAM:                int(plan.RAM.ValueInt64()),
		Disks:              int(plan.Disks.ValueInt64()),
		Username:           plan.Username.ValueString(),
		Password:           plan.Password.ValueString(),
		PublicKey:          plan.PublicKey.ValueString(),
		CloudInit:          plan.CloudInit.ValueString(),
		PrivateNetworkUUID: plan.PrivateNetworkUUID.ValueString(),
		ReservePublicIP:    plan.ReservePublicIP.ValueBool(),
		Backup:             plan.Backup.ValueBool(),
	}

	created, err := r.client.CreateVM(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating VM", "Could not create VM: "+err.Error())
		return
	}

	vm, err := r.pollUntilStatus(ctx, created.UUID, desiredStatus)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error waiting for VM to become ready",
			fmt.Sprintf("VM %s was created but did not reach status %q in time: %s", created.UUID, desiredStatus, err.Error()),
		)
		return
	}

	plan.UUID = types.StringValue(vm.UUID)
	plan.Location = types.StringValue(location)
	plan.DesiredStatus = types.StringValue(desiredStatus)
	plan.FloatIPAddress = types.StringValue(vm.FloatIPAddress)
	plan.ReservePublicIP = types.BoolValue(vm.ReservePublicIP)
	plan.Backup = types.BoolValue(vm.Backup)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vmResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vm, err := r.client.GetVM(ctx, state.UUID.ValueString())
	if err != nil {
		if client.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading VM", "Could not read VM "+state.UUID.ValueString()+": "+err.Error())
		return
	}

	state.Name = types.StringValue(vm.Name)
	state.BillingAccountID = types.Int64Value(int64(vm.BillingAccountID))
	state.Location = types.StringValue(vm.Location)
	state.OSName = types.StringValue(vm.OSName)
	state.OSVersion = types.StringValue(vm.OSVersion)
	state.VCPU = types.Int64Value(int64(vm.VCPU))
	state.RAM = types.Int64Value(int64(vm.RAM))
	state.Disks = types.Int64Value(int64(vm.Disks))
	state.Username = types.StringValue(vm.Username)
	state.PrivateNetworkUUID = types.StringValue(vm.PrivateNetworkUUID)
	state.FloatIPAddress = types.StringValue(vm.FloatIPAddress)
	state.ReservePublicIP = types.BoolValue(vm.ReservePublicIP)
	state.Backup = types.BoolValue(vm.Backup)

	// Only reconcile desired_status from a stable power state; transitional
	// states (queued/building/installing/deleting) don't map cleanly onto
	// the running/stopped intent and would otherwise thrash the plan.
	if vm.Status == vmStatusRunning || vm.Status == vmStatusStopped {
		state.DesiredStatus = types.StringValue(vm.Status)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is not yet implemented. PROVIDER_DESIGN.md §3.1 documents the
// full mutability model (name is online, vcpu/ram require a stop/start
// cycle, float_ip_address is reassigned via the IP endpoints), but wiring
// that up is scoped to a later session (see PROVIDER_DESIGN.md §7 step 4).
// Attributes that are safe to change in the plan without ForceNew are
// still declared as such so the schema matches the target design; until
// this method is implemented, any actual change to them fails loudly here
// instead of silently dropping the change.
func (r *vmResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"VM Updates Not Yet Implemented",
		"This session of the provider only implements idcloudhost_vm create/read/delete. "+
			"Updating an existing VM (name, vcpu, ram, float_ip_address, desired_status, backup) "+
			"is planned for a later session per PROVIDER_DESIGN.md build order step 4. "+
			"To apply this change today, taint and recreate the resource.",
	)
}

func (r *vmResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, diags := state.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	uuid := state.UUID.ValueString()

	if err := r.client.DeleteVM(ctx, uuid); err != nil && !client.NotFound(err) {
		resp.Diagnostics.AddError("Error deleting VM", "Could not delete VM "+uuid+": "+err.Error())
		return
	}

	if err := r.pollUntilGone(ctx, uuid); err != nil {
		resp.Diagnostics.AddError(
			"Error waiting for VM to be deleted",
			fmt.Sprintf("VM %s was sent a delete request but did not disappear in time: %s", uuid, err.Error()),
		)
	}
}

// pollUntilStatus polls GetVM until it reports the target status, the
// context is cancelled (including timeout), or an unexpected error occurs.
func (r *vmResource) pollUntilStatus(ctx context.Context, uuid, targetStatus string) (*client.VM, error) {
	for {
		vm, err := r.client.GetVM(ctx, uuid)
		if err != nil {
			return nil, err
		}
		if vm.Status == targetStatus {
			return vm, nil
		}

		if err := sleepOrDone(ctx, pollInterval); err != nil {
			return nil, err
		}
	}
}

// pollUntilGone polls GetVM until it 404s (deleted), the context is
// cancelled (including timeout), or an unexpected error occurs.
func (r *vmResource) pollUntilGone(ctx context.Context, uuid string) error {
	for {
		_, err := r.client.GetVM(ctx, uuid)
		if err != nil {
			if client.NotFound(err) {
				return nil
			}
			return err
		}

		if err := sleepOrDone(ctx, pollInterval); err != nil {
			return err
		}
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
