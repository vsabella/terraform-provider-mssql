package provider

import (
	"context"
	"fmt"
	"net/url"
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
				MarkdownDescription: "Resource identifier in format `<server_id>/<database>/<role>` where `server_id` is `host:port`.",
				Computed:            true,
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
			fmt.Sprintf("Expected *core.ProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
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

	data.Id = types.StringValue(fmt.Sprintf("%s/%s/%s", r.ctx.ServerID, database, role.Name))
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
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" || data.Name.IsNull() || data.Name.ValueString() == "" {
		dbName, roleName, err := parseRoleId(data.Id.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid role ID", err.Error())
			return
		}
		database = dbName
		data.Name = types.StringValue(roleName)
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

	data.Id = types.StringValue(fmt.Sprintf("%s/%s/%s", r.ctx.ServerID, database, role.Name))
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
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" || data.Name.IsNull() || data.Name.ValueString() == "" {
		dbName, roleName, err := parseRoleId(data.Id.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid role ID", err.Error())
			return
		}
		database = dbName
		data.Name = types.StringValue(roleName)
	}

	err := r.ctx.Client.DeleteRole(ctx, database, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("unable to delete role", fmt.Sprintf("unable to delete role %s, got error: %s", data.Id.ValueString(), err))
		return
	}
}

func (r *MssqlRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import ID must be <server_id>/<database>/<role>
	database, name, err := parseRoleId(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), database)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/%s/%s", r.ctx.ServerID, database, name))...)
}

func parseRoleId(id string) (string, string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", fmt.Errorf("expected id in format <server_id>/<database>/<role>, got %q", id)
	}
	db, err := url.QueryUnescape(parts[1])
	if err != nil {
		return "", "", err
	}
	name, err := url.QueryUnescape(parts[2])
	if err != nil {
		return "", "", err
	}
	return db, name, nil
}
