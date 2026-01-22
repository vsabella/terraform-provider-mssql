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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/vsabella/terraform-provider-mssql/internal/core"
	"github.com/vsabella/terraform-provider-mssql/internal/mssql"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &MssqlUserResource{}
var _ resource.ResourceWithImportState = &MssqlUserResource{}

func NewMssqlUserResource() resource.Resource {
	return &MssqlUserResource{}
}

type MssqlUserResource struct {
	ctx core.ProviderData
}

type MssqlUserResourceModel struct {
	Id            types.String `tfsdk:"id"`
	Database      types.String `tfsdk:"database"`
	Username      types.String `tfsdk:"username"`
	Password      types.String `tfsdk:"password"`
	External      types.Bool   `tfsdk:"external"`
	Sid           types.String `tfsdk:"sid"`
	DefaultSchema types.String `tfsdk:"default_schema"`
	LoginName     types.String `tfsdk:"login_name"`
}

func (r *MssqlUserResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *MssqlUserResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a SQL Server database user. Supports both contained users (with password) and login-based users (mapped to a server login).",

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
			"username": schema.StringAttribute{
				MarkdownDescription: "Database user name. Changing this forces a new resource to be created.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"password": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Password for contained database users. Must follow strong password policies defined for SQL server. " +
					"Passwords are case-sensitive, length must be 8-128 chars, can include all characters except `'` or `name`.\n\n" +
					"~> **Note** Password will be stored in the raw state as plain-text. [Read more about sensitive data in state](https://www.terraform.io/language/state/sensitive-data).\n\n" +
					"~> **Note** Either `password` or `login_name` must be specified, but not both. Use `password` for contained database users (Azure SQL) or `login_name` for traditional login-mapped users (RDS SQL Server).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"login_name": schema.StringAttribute{
				MarkdownDescription: "Name of the server login to map this user to. Use this for traditional login-based users (e.g., RDS SQL Server). " +
					"When set, the user is created with `CREATE USER ... FOR LOGIN ...`. Mutually exclusive with `password`.",
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"external": schema.BoolAttribute{
				MarkdownDescription: "Is this an external user (like Microsoft EntraID). Mutually exclusive with `password` and `login_name`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"sid": schema.StringAttribute{
				MarkdownDescription: "Set custom SID for the user.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"default_schema": schema.StringAttribute{
				MarkdownDescription: "Default schema for the user. Defaults to `dbo`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("dbo"),
			},
		},
	}
}

func (r *MssqlUserResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *MssqlUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MssqlUserResourceModel

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

	hasPassword := !data.Password.IsNull() && data.Password.ValueString() != ""
	hasLoginName := !data.LoginName.IsNull() && data.LoginName.ValueString() != ""
	isExternal := data.External.ValueBool()

	if hasPassword && hasLoginName {
		resp.Diagnostics.AddError("Invalid configuration",
			"Cannot specify both 'password' and 'login_name'. Use 'password' for contained database users or 'login_name' for login-based users.")
		return
	}

	if isExternal && (hasPassword || hasLoginName) {
		resp.Diagnostics.AddError("Invalid configuration",
			"External users cannot have 'password' or 'login_name' specified.")
		return
	}

	if !hasPassword && !hasLoginName && !isExternal {
		resp.Diagnostics.AddError("Invalid configuration",
			"Either 'password', 'login_name', or 'external = true' must be specified.")
		return
	}

	create := mssql.CreateUser{
		Username:      data.Username.ValueString(),
		Password:      data.Password.ValueString(),
		LoginName:     data.LoginName.ValueString(),
		Sid:           data.Sid.ValueString(),
		External:      data.External.ValueBool(),
		DefaultSchema: data.DefaultSchema.ValueString(),
	}

	user, err := r.ctx.Client.CreateUser(ctx, database, create)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error creating user %s", create.Username), err.Error())
		return
	}

	userToResource(&data, r.ctx.ServerID, database, user)
	tflog.Debug(ctx, fmt.Sprintf("Created user %s", data.Username))

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func userToResource(data *MssqlUserResourceModel, serverID string, database string, user mssql.User) {
	data.Id = types.StringValue(fmt.Sprintf("%s/%s/%s", serverID, database, user.Username))
	data.Database = types.StringValue(database)
	data.Username = types.StringValue(user.Username)

	if user.LoginName != "" {
		data.LoginName = types.StringValue(user.LoginName)
	} else {
		data.LoginName = types.StringNull()
	}

	if user.Sid != "" {
		data.Sid = types.StringValue(user.Sid)
	}

	data.External = types.BoolValue(user.External)
	data.DefaultSchema = types.StringValue(user.DefaultSchema)
}

func (r *MssqlUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MssqlUserResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		if id := data.Id.ValueString(); id != "" {
			parts := strings.Split(id, "/")
			switch len(parts) {
			case 2:
				if parts[0] == r.ctx.ServerID {
					database = r.ctx.Database
					data.Username = types.StringValue(parts[1])
				} else {
					database = parts[0]
					data.Username = types.StringValue(parts[1])
				}
			case 3:
				database = parts[1]
				data.Username = types.StringValue(parts[2])
			}
		}
		if database == "" {
			database = r.ctx.Database
		}
	}

	user, err := r.ctx.Client.GetUser(ctx, database, data.Username.ValueString())

	// If resource is not found, remove it from the state
	if errors.Is(err, sql.ErrNoRows) {
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable", fmt.Sprintf("Unable to read MssqlUser, got error: %s", err))
		return
	}

	userToResource(&data, r.ctx.ServerID, database, user)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data MssqlUserResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	user := mssql.UpdateUser{
		Id:            data.Username.ValueString(),
		Password:      data.Password.ValueString(),
		DefaultSchema: data.DefaultSchema.ValueString(),
	}

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		database = r.ctx.Database
		data.Database = types.StringValue(database)
	}

	cur, err := r.ctx.Client.UpdateUser(ctx, database, user)
	if err != nil {
		resp.Diagnostics.AddError("could not update user", err.Error())
		return
	}

	data.Id = types.StringValue(fmt.Sprintf("%s/%s/%s", r.ctx.ServerID, database, cur.Username))
	data.Database = types.StringValue(database)
	data.DefaultSchema = types.StringValue(cur.DefaultSchema)

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MssqlUserResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		if id := data.Id.ValueString(); id != "" {
			parts := strings.Split(id, "/")
			switch len(parts) {
			case 2:
				if parts[0] == r.ctx.ServerID {
					database = r.ctx.Database
					data.Username = types.StringValue(parts[1])
				} else {
					database = parts[0]
					data.Username = types.StringValue(parts[1])
				}
			case 3:
				database = parts[1]
				data.Username = types.StringValue(parts[2])
			}
		}
		if database == "" {
			database = r.ctx.Database
		}
	}

	err := r.ctx.Client.DeleteUser(ctx, database, data.Username.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("unable to delete user", fmt.Sprintf("unable to delete user %s, got error: %s", data.Username.ValueString(), err))
		return
	}
}

func (r *MssqlUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import formats:
	// - <username> (uses provider database)
	// - <database>/<username>
	// - <server_id>/<username>
	// - <server_id>/<database>/<username>
	parts := strings.Split(req.ID, "/")
	var database, username string

	switch len(parts) {
	case 1:
		database = r.ctx.Database
		username = parts[0]
	case 2:
		if parts[0] == r.ctx.ServerID {
			database = r.ctx.Database
			username = parts[1]
		} else {
			database = parts[0]
			username = parts[1]
		}
	case 3:
		database = parts[1]
		username = parts[2]
	default:
		resp.Diagnostics.AddError("Invalid import ID", "expected <username>, <database>/<username>, <server_id>/<username>, or <server_id>/<database>/<username>")
		return
	}

	if database == "" || username == "" {
		resp.Diagnostics.AddError("Invalid import ID", "database and username must not be empty")
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), database)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("username"), username)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/%s/%s", r.ctx.ServerID, database, username))...)
}
