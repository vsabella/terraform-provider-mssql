package provider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/vsabella/terraform-provider-mssql/internal/core"
	"github.com/vsabella/terraform-provider-mssql/internal/mssql"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &MssqlPermissionResource{}
var _ resource.ResourceWithImportState = &MssqlPermissionResource{}

func NewMssqlPermissionResource() resource.Resource {
	return &MssqlPermissionResource{}
}

type MssqlPermissionResource struct {
	ctx core.ProviderData
}

type MssqlPermissionResourceModel struct {
	Id         types.String `tfsdk:"id"`
	Principal  types.String `tfsdk:"principal"`
	Permission types.String `tfsdk:"permission"`
}

func (r *MssqlPermissionResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_permission"
}

func (r *MssqlPermissionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "DB permission resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "`<principal>/<permission>`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"principal": schema.StringAttribute{
				MarkdownDescription: "`<principal>`.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"permission": schema.StringAttribute{
				MarkdownDescription: "Name of database-level SQL permission. For full list of supported permissions, see [docs](https://learn.microsoft.com/en-us/sql/t-sql/statements/grant-database-permissions-transact-sql?view=azuresqldb-current#remarks)",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *MssqlPermissionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *MssqlPermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MssqlPermissionResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	perm, err := r.ctx.Client.ReadDatabasePermission(ctx, data.Id.ValueString())

	// If resource is not found, remove it from the state
	if errors.Is(err, sql.ErrNoRows) {
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable", fmt.Sprintf("Unable to read Permission, got error: %s", err))
		return
	}

	data.Id = types.StringValue(perm.Id)
	data.Principal = types.StringValue(perm.Principal)
	data.Permission = types.StringValue(perm.Permission)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlPermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MssqlPermissionResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	perm, err := r.ctx.Client.GrantDatabasePermission(ctx, data.Principal.ValueString(), strings.ToUpper(data.Permission.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error granting permission %s to principal %s", data.Permission.ValueString(), data.Principal.ValueString()), err.Error())
		return
	}

	data.Id = types.StringValue(perm.Id)
	tflog.Debug(ctx, fmt.Sprintf("Granted permssion %s to principal %s", data.Permission, data.Principal))

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

}

func (r *MssqlPermissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data MssqlPermissionResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Normalize `data` here
	permission := mssql.DatabasePermission{
		Id:         data.Id.ValueString(),
		Principal:  data.Principal.ValueString(),
		Permission: strings.ToUpper(data.Permission.ValueString()),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &permission)...)
}

func (r *MssqlPermissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MssqlPermissionResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.ctx.Client.RevokeDatabasePermission(ctx, data.Principal.ValueString(), strings.ToUpper(data.Permission.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Unable to revoke permission", fmt.Sprintf("Unable to revoke permission %s from principal %s", data.Permission.ValueString(), data.Principal.ValueString()))
		return
	}
}

func (r *MssqlPermissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
