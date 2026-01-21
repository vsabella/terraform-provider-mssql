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
	Id        types.String `tfsdk:"id"`
	Database  types.String `tfsdk:"database"`
	Role      types.String `tfsdk:"role"`
	Principal types.String `tfsdk:"principal"`
}

func (r *MssqlRoleAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

func (r *MssqlRoleAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "MssqlUser resource",

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
			fmt.Sprintf("Expected *core.SqlClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
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

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		database = r.ctx.Database
		data.Database = types.StringValue(database)
	}

	membership, err := r.ctx.Client.AssignRole(ctx, database, data.Role.ValueString(), data.Principal.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error assigning role %s to principal %s", data.Role.ValueString(), data.Principal.ValueString()), err.Error())
		return
	}

	data.Id = types.StringValue(fmt.Sprintf("%s/%s", database, membership.Id))
	data.Role = types.StringValue(membership.Role)
	data.Principal = types.StringValue(membership.Member)
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

	database := data.Database.ValueString()
	membershipID := data.Id.ValueString()
	if db, rest, found := strings.Cut(membershipID, "/"); found {
		// If id is <database>/<encoded_membership_id>, prefer parsing it.
		if database == "" {
			database = db
			data.Database = types.StringValue(database)
		}
		membershipID = rest
	}
	if database == "" {
		database = r.ctx.Database
	}

	membership, err := r.ctx.Client.ReadRoleMembership(ctx, database, membershipID)

	// If resource is not found, remove it from the state
	if errors.Is(err, sql.ErrNoRows) {
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable", fmt.Sprintf("Unable to read MssqlUser, got error: %s", err))
		return
	}

	data.Id = types.StringValue(fmt.Sprintf("%s/%s", database, membership.Id))
	data.Database = types.StringValue(database)
	data.Role = types.StringValue(membership.Role)
	data.Principal = types.StringValue(membership.Member)
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

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		database = r.ctx.Database
	}

	err := r.ctx.Client.UnassignRole(ctx, database, data.Role.ValueString(), data.Principal.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("unable to unassign role", fmt.Sprintf("unable to unassign role %s from principal %s, got error: %s", data.Role.ValueString(), data.Principal.ValueString(), err))
		return
	}
}

func (r *MssqlRoleAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import formats:
	// - <role>/<principal> (uses provider database)
	// - <database>/<role>/<principal>
	parts := strings.Split(req.ID, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), r.ctx.Database)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal"), parts[1])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/%s/%s", r.ctx.Database, parts[0], parts[1]))...)
		return
	}
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role"), parts[1])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal"), parts[2])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/%s/%s", parts[0], parts[1], parts[2]))...)
		return
	}

	resp.Diagnostics.AddError("Invalid import ID", "expected <role>/<principal> or <database>/<role>/<principal>")
}
