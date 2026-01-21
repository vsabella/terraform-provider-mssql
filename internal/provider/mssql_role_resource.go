package provider

import (
	"context"
	"fmt"
	"strings"

	"database/sql"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/vsabella/terraform-provider-mssql/internal/core"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &MssqlRoleResource{}
var _ resource.ResourceWithImportState = &MssqlRoleResource{}

func NewMssqlRoleResource() resource.Resource {
	return &MssqlRoleResource{}
}

type MssqlRoleResource struct {
	ctx core.ProviderData
}

type MssqlRoleResourceModel struct {
	Id types.String `tfsdk:"id"`
	// Database is the target database. If omitted, uses provider database.
	Database types.String `tfsdk:"database"`
	Name     types.String `tfsdk:"name"`
}

func (r *MssqlRoleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *MssqlRoleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "MssqlRole resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"database": schema.StringAttribute{
				MarkdownDescription: "Target database. If not specified, uses the provider's configured database.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *MssqlRoleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *MssqlRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MssqlRoleResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		database = r.ctx.Database
		data.Database = types.StringValue(database)
	}

	role, err := r.ctx.Client.CreateRole(ctx, database, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error creating role %s", data.Id.ValueString()), err.Error())
		return
	}

	data.Id = types.StringValue(fmt.Sprintf("%s/%s", database, role.Name))
	data.Name = types.StringValue(role.Name)
	tflog.Debug(ctx, fmt.Sprintf("Created role %s", data.Id))

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data MssqlRoleResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MssqlRoleResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		database = r.ctx.Database
	}

	role, err := r.ctx.Client.GetRole(ctx, database, data.Name.ValueString())

	// If resource is not found, remove it from the state
	if errors.Is(err, sql.ErrNoRows) {
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable", fmt.Sprintf("Unable to read MssqlRole, got error: %s", err))
		return
	}

	data.Id = types.StringValue(fmt.Sprintf("%s/%s", database, role.Name))
	data.Database = types.StringValue(database)
	data.Name = types.StringValue(role.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MssqlRoleResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		database = r.ctx.Database
	}

	err := r.ctx.Client.DeleteRole(ctx, database, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("unable to delete role", fmt.Sprintf("unable to delete role %s, got error: %s", data.Id.ValueString(), err))
		return
	}
}

func (r *MssqlRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import formats:
	// - <role> (uses provider database)
	// - <database>/<role>
	parts := strings.Split(req.ID, "/")
	if len(parts) == 1 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), r.ctx.Database)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/%s", r.ctx.Database, parts[0]))...)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
		return
	}

	resp.Diagnostics.AddError("Invalid import ID", "expected <role> or <database>/<role>")
}
