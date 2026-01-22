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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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

func grantToId(serverID string, grant mssql.GrantPermission) string {
	parts := []string{
		url.QueryEscape(serverID),
		url.QueryEscape(grant.Database),
		url.QueryEscape(grant.Principal),
		url.QueryEscape(strings.ToUpper(grant.Permission)),
	}
	if grant.ObjectType != "" && grant.ObjectName != "" {
		parts = append(parts,
			url.QueryEscape(strings.ToUpper(grant.ObjectType)),
			url.QueryEscape(grant.ObjectName),
		)
	}
	return strings.Join(parts, "/")
}

func decodeGrantId(id string) (mssql.GrantPermission, error) {
	parts := strings.Split(id, "/")
	if len(parts) < 4 {
		return mssql.GrantPermission{}, fmt.Errorf("expected id in format <server_id>/<database>/<principal>/<permission>[/object_type/object_name], got %q", id)
	}

	switch len(parts) {
	case 4, 6:
		// ok
	default:
		return mssql.GrantPermission{}, fmt.Errorf("expected id in format <server_id>/<database>/<principal>/<permission>[/object_type/object_name], got %q", id)
	}

	decode := func(s string) (string, error) {
		return url.QueryUnescape(s)
	}

	serverID, err := decode(parts[0])
	if err != nil {
		return mssql.GrantPermission{}, err
	}
	if serverID == "" {
		return mssql.GrantPermission{}, fmt.Errorf("expected id in format <server_id>/<database>/<principal>/<permission>[/object_type/object_name], got %q", id)
	}

	db, err := decode(parts[1])
	if err != nil {
		return mssql.GrantPermission{}, err
	}
	principal, err := decode(parts[2])
	if err != nil {
		return mssql.GrantPermission{}, err
	}
	permission, err := decode(parts[3])
	if err != nil {
		return mssql.GrantPermission{}, err
	}

	grant := mssql.GrantPermission{
		Database:   db,
		Principal:  principal,
		Permission: permission,
	}

	if len(parts) == 6 {
		objectType, err := decode(parts[4])
		if err != nil {
			return mssql.GrantPermission{}, err
		}
		objectName, err := decode(parts[5])
		if err != nil {
			return mssql.GrantPermission{}, err
		}
		grant.ObjectType = objectType
		grant.ObjectName = objectName
	}

	return grant, nil
}

func (r *MssqlGrantResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_grant"
}

func (r *MssqlGrantResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "DB grant resource",

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
				MarkdownDescription: "Permission to grant (e.g., SELECT, EXECUTE, CONTROL, CREATE PROCEDURE).",
				Required:            true,
				Validators: []validator.String{
					databasePermissionValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"principal": schema.StringAttribute{
				MarkdownDescription: "Database principal (user or role) to grant permission to.",
				Required:            true,
				Validators: []validator.String{
					principalNameValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"object_type": schema.StringAttribute{
				MarkdownDescription: "Type of object to grant permission on (e.g., SCHEMA, TABLE, VIEW, PROCEDURE). If not specified, grants a database-level permission.",
				Optional:            true,
				Validators: []validator.String{
					objectTypeValidator{},
				},
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

func (r *MssqlGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MssqlGrantResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		database = r.ctx.Database
		data.Database = types.StringValue(database)
	}

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

	data.Id = types.StringValue(grantToId(r.ctx.ServerID, result))
	data.Database = types.StringValue(database)
	data.Principal = types.StringValue(result.Principal)
	data.Permission = types.StringValue(result.Permission)
	if result.ObjectType != "" {
		data.ObjectType = types.StringValue(result.ObjectType)
	}
	if result.ObjectName != "" {
		data.ObjectName = types.StringValue(result.ObjectName)
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
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		decoded, err := decodeGrantId(data.Id.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid grant ID", err.Error())
			return
		}
		database = decoded.Database
		if decoded.Principal != "" {
			data.Principal = types.StringValue(decoded.Principal)
		}
		if decoded.Permission != "" {
			data.Permission = types.StringValue(strings.ToUpper(decoded.Permission))
		}
		if decoded.ObjectType != "" {
			data.ObjectType = types.StringValue(decoded.ObjectType)
		}
		if decoded.ObjectName != "" {
			data.ObjectName = types.StringValue(decoded.ObjectName)
		}
	}

	hasObjectType := !data.ObjectType.IsNull() && data.ObjectType.ValueString() != ""
	hasObjectName := !data.ObjectName.IsNull() && data.ObjectName.ValueString() != ""
	if hasObjectType != hasObjectName {
		resp.Diagnostics.AddError("Invalid grant state",
			"Both 'object_type' and 'object_name' must be specified together, or neither.")
		return
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
	data.Database = types.StringValue(database)
	data.Principal = types.StringValue(perm.Principal)
	data.Permission = types.StringValue(perm.Permission)
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

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MssqlGrantResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		decoded, err := decodeGrantId(data.Id.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid grant ID", err.Error())
			return
		}
		database = decoded.Database
		if data.Principal.IsNull() && decoded.Principal != "" {
			data.Principal = types.StringValue(decoded.Principal)
		}
		if data.Permission.IsNull() && decoded.Permission != "" {
			data.Permission = types.StringValue(strings.ToUpper(decoded.Permission))
		}
		if data.ObjectType.IsNull() && decoded.ObjectType != "" {
			data.ObjectType = types.StringValue(decoded.ObjectType)
		}
		if data.ObjectName.IsNull() && decoded.ObjectName != "" {
			data.ObjectName = types.StringValue(decoded.ObjectName)
		}
	}

	grant := mssql.GrantPermission{
		Database:   database,
		Principal:  data.Principal.ValueString(),
		Permission: strings.ToUpper(data.Permission.ValueString()),
		ObjectType: strings.ToUpper(data.ObjectType.ValueString()),
		ObjectName: data.ObjectName.ValueString(),
	}

	if err := r.ctx.Client.RevokePermission(ctx, grant); err != nil {
		resp.Diagnostics.AddError("Unable to revoke permission", fmt.Sprintf("Unable to revoke permission %s from principal %s", data.Permission.ValueString(), data.Principal.ValueString()))
		return
	}
}

func (r *MssqlGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	grant, err := decodeGrantId(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	if grant.Database == "" || grant.Principal == "" || grant.Permission == "" {
		resp.Diagnostics.AddError("Invalid import ID", "database, principal, and permission must not be empty")
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), grant.Database)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal"), grant.Principal)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("permission"), strings.ToUpper(grant.Permission))...)
	if grant.ObjectType != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("object_type"), grant.ObjectType)...)
	}
	if grant.ObjectName != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("object_name"), grant.ObjectName)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), grantToId(r.ctx.ServerID, grant))...)
}
