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
	Database   types.String `tfsdk:"database"`
	Role       types.String `tfsdk:"role"`
	Principal  types.String `tfsdk:"principal"`
	ServerRole types.Bool   `tfsdk:"server_role"`
}

func (r *MssqlRoleAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

func (r *MssqlRoleAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
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
				MarkdownDescription: "Resource identifier in format `<server_id>/db/<database>/<role>/<principal>` or `<server_id>/server/<role>/<principal>` where `server_id` is `host:port`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
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
				MarkdownDescription: "Target database for database role assignments. If not specified, uses the provider's configured database. Ignored when `server_role = true`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"role": schema.StringAttribute{
				MarkdownDescription: "Role to to assign",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"principal": schema.StringAttribute{
				MarkdownDescription: "Database principal (e.g. username) to assign the role to",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *MssqlRoleAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *MssqlRoleAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MssqlRoleAssignmentResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	isServer := data.ServerRole.ValueBool()

	var membership mssql.RoleMembership
	var err error

	if isServer {
		membership, err = r.ctx.Client.AssignServerRole(ctx, data.Role.ValueString(), data.Principal.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Error assigning server role %s to principal %s", data.Role.ValueString(), data.Principal.ValueString()), err.Error())
			return
		}
		data.Database = types.StringNull()
		data.Id = types.StringValue(fmt.Sprintf("%s/server/%s/%s", r.ctx.ServerID, membership.Role, membership.Member))
	} else {
		database := data.Database.ValueString()
		if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
			database = r.ctx.Database
			data.Database = types.StringValue(database)
		}
		membership, err = r.ctx.Client.AssignRole(ctx, database, data.Role.ValueString(), data.Principal.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Error assigning role %s to principal %s", data.Role.ValueString(), data.Principal.ValueString()), err.Error())
			return
		}
		data.Id = types.StringValue(fmt.Sprintf("%s/db/%s/%s/%s", r.ctx.ServerID, database, membership.Role, membership.Member))
	}

	data.Role = types.StringValue(membership.Role)
	data.Principal = types.StringValue(membership.Member)
	data.ServerRole = types.BoolValue(isServer)
	tflog.Debug(ctx, fmt.Sprintf("Assigned role %s to principal %s with id %s", data.Role, data.Principal, data.Id))

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MssqlRoleAssignmentResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.Id.ValueString()
	isServer := data.ServerRole.ValueBool()

	// Parse ID if fields are missing.
	if id != "" && (data.Role.IsNull() || data.Role.ValueString() == "" || data.Principal.IsNull() || data.Principal.ValueString() == "" || data.ServerRole.IsNull() || (data.Database.IsNull() && !isServer)) {
		parsed, err := parseRoleAssignmentId(id)
		if err != nil {
			resp.Diagnostics.AddError("Invalid role assignment ID", err.Error())
			return
		}
		isServer = parsed.IsServer
		data.Role = types.StringValue(parsed.Role)
		data.Principal = types.StringValue(parsed.Principal)
		if parsed.Database != "" {
			data.Database = types.StringValue(parsed.Database)
		} else {
			data.Database = types.StringNull()
		}
	}

	var membership mssql.RoleMembership
	var err error
	if isServer {
		membership, err = r.ctx.Client.ReadServerRoleMembership(ctx, data.Role.ValueString(), data.Principal.ValueString())
	} else {
		database := data.Database.ValueString()
		if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
			database = r.ctx.Database
			data.Database = types.StringValue(database)
		}
		membershipID := fmt.Sprintf("%s/%s", data.Role.ValueString(), data.Principal.ValueString())
		membership, err = r.ctx.Client.ReadRoleMembership(ctx, database, membershipID)
	}

	// If resource is not found, remove it from the state
	if errors.Is(err, sql.ErrNoRows) {
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable", fmt.Sprintf("Unable to read MssqlUser, got error: %s", err))
		return
	}

	if isServer {
		data.Id = types.StringValue(fmt.Sprintf("%s/server/%s/%s", r.ctx.ServerID, membership.Role, membership.Member))
		data.Database = types.StringNull()
	} else {
		data.Id = types.StringValue(fmt.Sprintf("%s/db/%s/%s/%s", r.ctx.ServerID, data.Database.ValueString(), membership.Role, membership.Member))
	}
	data.Role = types.StringValue(membership.Role)
	data.Principal = types.StringValue(membership.Member)
	data.ServerRole = types.BoolValue(isServer)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data MssqlRoleAssignmentResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MssqlRoleAssignmentResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.Id.ValueString()
	isServer := data.ServerRole.ValueBool()

	if id != "" && (data.Role.IsNull() || data.Principal.IsNull() || data.ServerRole.IsNull() || (data.Database.IsNull() && !isServer)) {
		parsed, err := parseRoleAssignmentId(id)
		if err != nil {
			resp.Diagnostics.AddError("Invalid role assignment ID", err.Error())
			return
		}
		isServer = parsed.IsServer
		data.Role = types.StringValue(parsed.Role)
		data.Principal = types.StringValue(parsed.Principal)
		if parsed.Database != "" {
			data.Database = types.StringValue(parsed.Database)
		} else {
			data.Database = types.StringNull()
		}
	}

	if isServer {
		if err := r.ctx.Client.UnassignServerRole(ctx, data.Role.ValueString(), data.Principal.ValueString()); err != nil {
			resp.Diagnostics.AddError("unable to unassign server role", fmt.Sprintf("unable to unassign server role %s from principal %s, got error: %s", data.Role.ValueString(), data.Principal.ValueString(), err))
			return
		}
		return
	}

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		database = r.ctx.Database
	}

	if err := r.ctx.Client.UnassignRole(ctx, database, data.Role.ValueString(), data.Principal.ValueString()); err != nil {
		resp.Diagnostics.AddError("unable to unassign role", fmt.Sprintf("unable to unassign role %s from principal %s, got error: %s", data.Role.ValueString(), data.Principal.ValueString(), err))
		return
	}
}

func (r *MssqlRoleAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import ID must be:
	// - <server_id>/db/<database>/<role>/<principal>
	// - <server_id>/server/<role>/<principal>
	parsed, err := parseRoleAssignmentId(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role"), parsed.Role)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal"), parsed.Principal)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_role"), parsed.IsServer)...)
	if parsed.Database != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), parsed.Database)...)
	}

	if parsed.IsServer {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/server/%s/%s", r.ctx.ServerID, parsed.Role, parsed.Principal))...)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/db/%s/%s/%s", r.ctx.ServerID, parsed.Database, parsed.Role, parsed.Principal))...)
}

type roleAssignmentId struct {
	IsServer  bool
	Database  string
	Role      string
	Principal string
}

func parseRoleAssignmentId(id string) (roleAssignmentId, error) {
	parts := strings.Split(id, "/")
	if len(parts) < 4 || parts[0] == "" {
		return roleAssignmentId{}, fmt.Errorf("expected id in format <server_id>/db/<database>/<role>/<principal> or <server_id>/server/<role>/<principal>, got %q", id)
	}
	scope := parts[1]
	switch scope {
	case "server":
		if len(parts) != 4 {
			return roleAssignmentId{}, fmt.Errorf("expected id in format <server_id>/server/<role>/<principal>, got %q", id)
		}
		role, err := url.QueryUnescape(parts[2])
		if err != nil {
			return roleAssignmentId{}, err
		}
		principal, err := url.QueryUnescape(parts[3])
		if err != nil {
			return roleAssignmentId{}, err
		}
		if role == "" || principal == "" {
			return roleAssignmentId{}, fmt.Errorf("expected id in format <server_id>/server/<role>/<principal>, got %q", id)
		}
		return roleAssignmentId{IsServer: true, Role: role, Principal: principal}, nil
	case "db":
		if len(parts) != 5 {
			return roleAssignmentId{}, fmt.Errorf("expected id in format <server_id>/db/<database>/<role>/<principal>, got %q", id)
		}
		db, err := url.QueryUnescape(parts[2])
		if err != nil {
			return roleAssignmentId{}, err
		}
		role, err := url.QueryUnescape(parts[3])
		if err != nil {
			return roleAssignmentId{}, err
		}
		principal, err := url.QueryUnescape(parts[4])
		if err != nil {
			return roleAssignmentId{}, err
		}
		if db == "" || role == "" || principal == "" {
			return roleAssignmentId{}, fmt.Errorf("expected id in format <server_id>/db/<database>/<role>/<principal>, got %q", id)
		}
		return roleAssignmentId{IsServer: false, Database: db, Role: role, Principal: principal}, nil
	default:
		return roleAssignmentId{}, fmt.Errorf("expected id in format <server_id>/db/<database>/<role>/<principal> or <server_id>/server/<role>/<principal>, got %q", id)
	}
}
