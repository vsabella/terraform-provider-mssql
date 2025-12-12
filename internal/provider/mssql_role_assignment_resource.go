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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/vsabella/terraform-provider-mssql/internal/core"
	"github.com/vsabella/terraform-provider-mssql/internal/mssql"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &MssqlRoleAssignmentResource{}
var _ resource.ResourceWithImportState = &MssqlRoleAssignmentResource{}

func NewMssqlRoleAssignmentResource() resource.Resource {
	return &MssqlRoleAssignmentResource{}
}

type MssqlRoleAssignmentResource struct {
	ctx core.ProviderData
}

type MssqlRoleAssignmentResourceModel struct {
	Id         types.String `tfsdk:"id"`
	Role       types.String `tfsdk:"role"`
	Principal  types.String `tfsdk:"principal"`
	ServerRole types.Bool   `tfsdk:"server_role"`
	Database   types.String `tfsdk:"database"`
}

func (r *MssqlRoleAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

func (r *MssqlRoleAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `Assigns a principal to a database role or server role.

**Database role example:**
` + "```hcl" + `
resource "mssql_role_assignment" "db_reader" {
  database  = mssql_database.app.name
  role      = "db_datareader"
  principal = mssql_user.app.username
}
` + "```" + `

**Server role example (for telemetry):**
` + "```hcl" + `
resource "mssql_role_assignment" "telemetry_state_reader" {
  server_role = true
  role        = "##MS_ServerStateReader##"
  principal   = mssql_login.telemetry.name
}
` + "```",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"role": schema.StringAttribute{
				MarkdownDescription: "Name of the role to assign. For server roles, use names like `##MS_ServerStateReader##`, `##MS_DefinitionReader##`, `##MS_DatabaseConnector##`.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"principal": schema.StringAttribute{
				MarkdownDescription: "Principal to assign to the role. For database roles, this is a database user. For server roles (`server_role = true`), this is a login.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"server_role": schema.BoolAttribute{
				MarkdownDescription: "If true, assigns to a server-level role (ALTER SERVER ROLE). If false (default), assigns to a database role (ALTER ROLE). When true, `database` is ignored.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"database": schema.StringAttribute{
				MarkdownDescription: "Target database for database role assignments. If not specified, uses the provider's default database. Ignored when `server_role = true`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *MssqlRoleAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *MssqlRoleAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MssqlRoleAssignmentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	isServer := data.ServerRole.ValueBool()

	var membership mssql.RoleMembership
	var err error

	if isServer {
		// Server role assignment
		membership, err = r.ctx.Client.AssignServerRole(ctx, data.Role.ValueString(), data.Principal.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				fmt.Sprintf("Error assigning server role %s to principal %s", data.Role.ValueString(), data.Principal.ValueString()),
				err.Error())
			return
		}
		tflog.Debug(ctx, fmt.Sprintf("Assigned server role %s to principal %s", data.Role.ValueString(), data.Principal.ValueString()))
		// Database is not applicable for server roles.
		data.Database = types.StringNull()
	} else {
		// Database role assignment
		database := data.Database.ValueString()
		if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
			database = r.ctx.Database
			data.Database = types.StringValue(database)
		}
		membership, err = r.ctx.Client.AssignRole(ctx, database, data.Role.ValueString(), data.Principal.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				fmt.Sprintf("Error assigning role %s to principal %s", data.Role.ValueString(), data.Principal.ValueString()),
				err.Error())
			return
		}
		tflog.Debug(ctx, fmt.Sprintf("Assigned database role %s to principal %s in database %s", data.Role.ValueString(), data.Principal.ValueString(), database))
	}

	// Compute canonical ID using provider ServerID (not the raw mssql client host),
	// so IDs remain globally unique and consistent across resources.
	if isServer {
		data.Id = types.StringValue(fmt.Sprintf("%s/server/%s/%s", r.ctx.ServerID, membership.Role, membership.Member))
	} else {
		data.Id = types.StringValue(fmt.Sprintf("%s/db/%s/%s/%s", r.ctx.ServerID, data.Database.ValueString(), membership.Role, membership.Member))
	}
	data.ServerRole = types.BoolValue(isServer)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MssqlRoleAssignmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	isServer := data.ServerRole.ValueBool()

	var membership mssql.RoleMembership
	var err error

	if isServer {
		// Server role
		membership, err = r.ctx.Client.ReadServerRoleMembership(ctx, data.Role.ValueString(), data.Principal.ValueString())
	} else {
		// Database role
		database := data.Database.ValueString()
		if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
			database = r.ctx.Database
			data.Database = types.StringValue(database)
		}
		membership, err = r.ctx.Client.ReadRoleMembership(ctx, database, data.Role.ValueString(), data.Principal.ValueString())
	}

	if errors.Is(err, sql.ErrNoRows) {
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable to read role membership", fmt.Sprintf("Error: %s", err))
		return
	}

	if isServer {
		data.Id = types.StringValue(fmt.Sprintf("%s/server/%s/%s", r.ctx.ServerID, membership.Role, membership.Member))
	} else {
		data.Id = types.StringValue(fmt.Sprintf("%s/db/%s/%s/%s", r.ctx.ServerID, data.Database.ValueString(), membership.Role, membership.Member))
	}
	data.Role = types.StringValue(membership.Role)
	data.Principal = types.StringValue(membership.Member)
	data.ServerRole = types.BoolValue(isServer)
	if isServer {
		data.Database = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data MssqlRoleAssignmentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Ensure server_role is concrete in state
	data.ServerRole = types.BoolValue(data.ServerRole.ValueBool())

	// All attributes require replace, so Update just writes state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MssqlRoleAssignmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	isServer := data.ServerRole.ValueBool()

	var err error
	if isServer {
		err = r.ctx.Client.UnassignServerRole(ctx, data.Role.ValueString(), data.Principal.ValueString())
	} else {
		database := data.Database.ValueString()
		if database == "" {
			database = r.ctx.Database
		}
		err = r.ctx.Client.UnassignRole(ctx, database, data.Role.ValueString(), data.Principal.ValueString())
	}

	if err != nil {
		resp.Diagnostics.AddError("Unable to unassign role",
			fmt.Sprintf("Unable to unassign role %s from principal %s: %s",
				data.Role.ValueString(), data.Principal.ValueString(), err.Error()))
		return
	}
}

func (r *MssqlRoleAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import ID (host + scope):
	//   Server role:   <server_id>/server/<role>/<principal>
	//   Database role: <server_id>/db/<database>/<role>/<principal>
	// role, principal, and database are URL-encoded

	parts := strings.Split(req.ID, "/")
	if len(parts) < 4 {
		resp.Diagnostics.AddError("Invalid import ID",
			"Import ID must be in format <server_id>/server/<role>/<principal> or <server_id>/db/<database>/<role>/<principal>")
		return
	}

	host := parts[0]
	scope := parts[1]

	var roleEnc, principalEnc, databaseEnc string
	isServer := false

	if scope == "server" && len(parts) == 4 {
		isServer = true
		roleEnc = parts[2]
		principalEnc = parts[3]
	} else if scope == "db" && len(parts) == 5 {
		databaseEnc = parts[2]
		roleEnc = parts[3]
		principalEnc = parts[4]
	} else {
		resp.Diagnostics.AddError("Invalid import ID",
			"Import ID must be in format <server_id>/server/<role>/<principal> or <server_id>/db/<database>/<role>/<principal>")
		return
	}

	role, err := url.QueryUnescape(roleEnc)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Failed to decode role: %s", err))
		return
	}

	principal, err := url.QueryUnescape(principalEnc)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Failed to decode principal: %s", err))
		return
	}

	if host == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Host segment cannot be empty")
		return
	}

	if isServer {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_role"), true)...)
	} else {
		db, err := url.QueryUnescape(databaseEnc)
		if err != nil {
			resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Failed to decode database: %s", err))
			return
		}
		if db == "" {
			resp.Diagnostics.AddError("Invalid import ID", "Database segment cannot be empty for database role assignments")
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), db)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_role"), false)...)
	}

	// Store canonical ID using the provider's configured ServerID (including port).
	var canonicalID string
	if isServer {
		canonicalID = fmt.Sprintf("%s/server/%s/%s", r.ctx.ServerID, role, principal)
	} else {
		db := ""
		if databaseEnc != "" {
			if decodedDb, derr := url.QueryUnescape(databaseEnc); derr == nil {
				db = decodedDb
			}
		}
		if db == "" {
			resp.Diagnostics.AddError("Invalid import ID", "Database segment cannot be empty for database role assignments")
			return
		}
		canonicalID = fmt.Sprintf("%s/db/%s/%s/%s", r.ctx.ServerID, db, role, principal)
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), canonicalID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role"), role)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal"), principal)...)
}
