package provider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
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
var _ resource.Resource = &MssqlGrantResource{}
var _ resource.ResourceWithImportState = &MssqlGrantResource{}

func NewMssqlGrantResource() resource.Resource {
	return &MssqlGrantResource{}
}

type MssqlGrantResource struct {
	ctx core.ProviderData
}

type MssqlGrantResourceModel struct {
	Id         types.String `tfsdk:"id"`
	Database   types.String `tfsdk:"database"`
	Permission types.String `tfsdk:"permission"`
	Principal  types.String `tfsdk:"principal"`
	ObjectType types.String `tfsdk:"object_type"`
	ObjectName types.String `tfsdk:"object_name"`
}

func (r *MssqlGrantResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_grant"
}

func (r *MssqlGrantResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `Grants permissions to a database principal.

Supports both database-level permissions (e.g., CREATE PROCEDURE) and object-level permissions (e.g., CONTROL on a SCHEMA).

**Examples:**

Database-level grant:
` + "```hcl" + `
resource "mssql_grant" "create_proc" {
  database   = "mydb"
  permission = "CREATE PROCEDURE"
  principal  = "app_user"
}
` + "```" + `

Schema-level grant:
` + "```hcl" + `
resource "mssql_grant" "schema_control" {
  database    = "mydb"
  permission  = "CONTROL"
  principal   = "tools_user"
  object_type = "SCHEMA"
  object_name = "tools"
}
` + "```",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Resource identifier in format `<server_id>/<database>/<principal>/<permission>[/object_type/object_name]` where `server_id` is `host:port`.",
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
			"permission": schema.StringAttribute{
				MarkdownDescription: "Permission to grant (e.g., SELECT, EXECUTE, CONTROL, CREATE PROCEDURE). See [database permissions](https://learn.microsoft.com/en-us/sql/t-sql/statements/grant-database-permissions-transact-sql) and [schema permissions](https://learn.microsoft.com/en-us/sql/t-sql/statements/grant-schema-permissions-transact-sql).",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"principal": schema.StringAttribute{
				MarkdownDescription: "Database principal (user or role) to grant permission to.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"object_type": schema.StringAttribute{
				MarkdownDescription: "Type of object to grant permission on (e.g., SCHEMA, TABLE, VIEW, PROCEDURE). If not specified, grants a database-level permission.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"object_name": schema.StringAttribute{
				MarkdownDescription: "Name of the object to grant permission on. Required if `object_type` is specified.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *MssqlGrantResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *MssqlGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MssqlGrantResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve effective database (explicit in state for clarity).
	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		database = r.ctx.Database
		data.Database = types.StringValue(database)
	}

	// Validate object_type and object_name
	hasObjectType := !data.ObjectType.IsNull() && data.ObjectType.ValueString() != ""
	hasObjectName := !data.ObjectName.IsNull() && data.ObjectName.ValueString() != ""

	if hasObjectType != hasObjectName {
		resp.Diagnostics.AddError("Invalid configuration",
			"Both 'object_type' and 'object_name' must be specified together, or neither.")
		return
	}

	grant := mssql.GrantPermission{
		Database:   database,
		Principal:  data.Principal.ValueString(),
		Permission: strings.ToUpper(data.Permission.ValueString()),
		ObjectType: strings.ToUpper(data.ObjectType.ValueString()),
		ObjectName: data.ObjectName.ValueString(),
	}

	result, err := r.ctx.Client.GrantPermission(ctx, grant)
	if err != nil {
		resp.Diagnostics.AddError(
			fmt.Sprintf("Error granting permission %s to principal %s", grant.Permission, grant.Principal),
			err.Error())
		return
	}

	// Update state with normalized values from result
	data.Id = types.StringValue(grantToId(r.ctx.ServerID, result))
	if result.ObjectType != "" {
		// Store the normalized object type (SCHEMA or OBJECT)
		data.ObjectType = types.StringValue(result.ObjectType)
	}
	tflog.Debug(ctx, fmt.Sprintf("Granted permission %s to principal %s (id: %s)", grant.Permission, grant.Principal, data.Id.ValueString()))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MssqlGrantResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	if database == "" {
		database = r.ctx.Database
		data.Database = types.StringValue(database)
	}

	lookupGrant := mssql.GrantPermission{
		Database:   database,
		Principal:  data.Principal.ValueString(),
		Permission: strings.ToUpper(data.Permission.ValueString()),
		ObjectType: strings.ToUpper(data.ObjectType.ValueString()),
		ObjectName: data.ObjectName.ValueString(),
	}
	perm, err := r.ctx.Client.ReadPermission(ctx, lookupGrant)

	if errors.Is(err, sql.ErrNoRows) {
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable to read grant", fmt.Sprintf("Error: %s", err))
		return
	}

	data.Id = types.StringValue(grantToId(r.ctx.ServerID, perm))
	data.Principal = types.StringValue(perm.Principal)
	data.Permission = types.StringValue(perm.Permission)
	// Keep database explicit in state for clarity.
	if perm.Database != "" {
		data.Database = types.StringValue(perm.Database)
	}
	if perm.ObjectType != "" {
		data.ObjectType = types.StringValue(perm.ObjectType)
	}
	if perm.ObjectName != "" {
		data.ObjectName = types.StringValue(perm.ObjectName)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlGrantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data MssqlGrantResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// All attributes require replace, so Update shouldn't be called
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MssqlGrantResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	if database == "" {
		database = r.ctx.Database
	}

	grant := mssql.GrantPermission{
		Database:   database,
		Principal:  data.Principal.ValueString(),
		Permission: strings.ToUpper(data.Permission.ValueString()),
		ObjectType: strings.ToUpper(data.ObjectType.ValueString()),
		ObjectName: data.ObjectName.ValueString(),
	}

	err := r.ctx.Client.RevokePermission(ctx, grant)
	if err != nil {
		resp.Diagnostics.AddError("Unable to revoke permission",
			fmt.Sprintf("Unable to revoke permission %s from principal %s: %s",
				grant.Permission, grant.Principal, err.Error()))
		return
	}
}

func (r *MssqlGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// ID format: <server_id>/<database>/<principal>/<permission>[/objecttype/objectname]
	parts := strings.Split(req.ID, "/")
	if len(parts) < 4 {
		resp.Diagnostics.AddError("Invalid import ID",
			"Import ID must be in format: <server_id>/<database>/<principal>/<permission> or <server_id>/<database>/<principal>/<permission>/<object_type>/<object_name>")
		return
	}

	db := parts[1]
	if db == "" {
		db = r.ctx.Database
	}

	principal, err := url.QueryUnescape(parts[2])
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Failed to decode principal: %s", err))
		return
	}
	permission, err := url.QueryUnescape(parts[3])
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Failed to decode permission: %s", err))
		return
	}

	var objectType, objectName string
	if len(parts) > 4 {
		if objectType, err = url.QueryUnescape(parts[4]); err != nil {
			resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Failed to decode object_type: %s", err))
			return
		}
	}
	if len(parts) > 5 {
		if objectName, err = url.QueryUnescape(parts[5]); err != nil {
			resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Failed to decode object_name: %s", err))
			return
		}
	}

	canonical := grantToId(r.ctx.ServerID, mssql.GrantPermission{
		Database:   db,
		Principal:  principal,
		Permission: permission,
		ObjectType: objectType,
		ObjectName: objectName,
	})

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), canonical)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), db)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal"), principal)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("permission"), permission)...)

	if len(parts) > 4 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("object_type"), objectType)...)
	}
	if len(parts) > 5 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("object_name"), objectName)...)
	}
}

func grantToId(serverID string, grant mssql.GrantPermission) string {
	parts := []string{
		serverID,
		grant.Database,
		grant.Principal,
		grant.Permission,
	}
	if grant.ObjectType != "" && grant.ObjectName != "" {
		parts = append(parts, grant.ObjectType, grant.ObjectName)
	}
	return strings.Join(parts, "/")
}
