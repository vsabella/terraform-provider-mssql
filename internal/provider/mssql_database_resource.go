package provider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/vsabella/terraform-provider-mssql/internal/core"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &MssqlDatabaseResource{}
var _ resource.ResourceWithImportState = &MssqlDatabaseResource{}
var resLock sync.Mutex

func NewMssqlDatabaseResource() resource.Resource {
	return &MssqlDatabaseResource{}
}

type MssqlDatabaseResource struct {
	ctx core.ProviderData
}

type MssqlDatabaseResourceModel struct {
	Id   types.Int64  `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func (r *MssqlDatabaseResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

func (r *MssqlDatabaseResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "MssqlDatabase resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "Database ID. Can be retrieved using `SELECT DB_ID('<db_name>')`.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Database name.",
				Required:            true,
			},
		},
	}
}

func (r *MssqlDatabaseResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*core.ProviderData)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *core.SqlClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.ctx = *client
}

func (r *MssqlDatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	resLock.Lock()
	defer resLock.Unlock()

	var data MssqlDatabaseResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	db, err := r.ctx.Client.CreateDatabase(ctx, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error creating database %s", data.Name.ValueString()), err.Error())
		return
	}

	data.Id = types.Int64Value(db.Id)
	tflog.Debug(ctx, fmt.Sprintf("Created database %s with id %d", data.Name.ValueString(), data.Id.ValueInt64()))

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlDatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state MssqlDatabaseResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	db, err := r.ctx.Client.GetDatabaseById(ctx, state.Id.ValueInt64())

	// If resource is not found, remove it from the state
	if errors.Is(err, sql.ErrNoRows) {
		resp.Diagnostics.AddWarning("Database not found, removing from state", fmt.Sprintf("Database %s (id: %d) not found, removing from state", state.Name.ValueString(), state.Id.ValueInt64()))
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable to read database", fmt.Sprintf("Unable to read database %s (id: %d). Error: %s", state.Name.ValueString(), state.Id.ValueInt64(), err))
		return
	}

	state.Id = types.Int64Value(db.Id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *MssqlDatabaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resLock.Lock()
	defer resLock.Unlock()

	var plan, state MssqlDatabaseResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// we don't support updating database name as there should not be any reason to do so.
	if plan.Name.ValueString() != state.Name.ValueString() {
		resp.Diagnostics.AddError("Unable to update database", fmt.Sprintf("Updating database name is not supported. Database name cannot be changed from %s to %s.", state.Name.ValueString(), plan.Name.ValueString()))
		return
	}

	// nothing changed, save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MssqlDatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resLock.Lock()
	defer resLock.Unlock()

	var data MssqlDatabaseResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// we don't support deleting database, otherwise, an unintentional deletion of a database could happen.
	resp.Diagnostics.AddError("Unable to delete database", fmt.Sprintf("Deleting a database is not supported. Database %s (id: %d) will not be deleted, contact the database administrator for this operation.", data.Name.ValueString(), data.Id.ValueInt64()))

	// nothing changed, recover the state back to the original state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlDatabaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
