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
var _ resource.Resource = &MssqlRoleResource{}
var _ resource.ResourceWithImportState = &MssqlRoleResource{}

func NewMssqlRoleResource() resource.Resource {
	return &MssqlRoleResource{}
}

type MssqlRoleResource struct {
	ctx core.ProviderData
}

type MssqlRoleResourceModel struct {
	Id       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Database types.String `tfsdk:"database"`
}

func (r *MssqlRoleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *MssqlRoleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates a database role in the specified database.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the role to create.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"database": schema.StringAttribute{
				MarkdownDescription: "Target database for the role. If not specified, uses the provider's default database.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *MssqlRoleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	role, err := r.ctx.Client.CreateRole(ctx, database, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error creating role %s", data.Name.ValueString()), err.Error())
		return
	}

	data.Id = types.StringValue(role.Id)
	tflog.Debug(ctx, fmt.Sprintf("Created role %s in database %s", data.Name.ValueString(), database))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data MssqlRoleResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Role rename not supported - all attributes require replace
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MssqlRoleResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	role, err := r.ctx.Client.GetRole(ctx, database, data.Id.ValueString())

	if errors.Is(err, sql.ErrNoRows) {
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable to read role", fmt.Sprintf("Unable to read role %s: %s", data.Id.ValueString(), err))
		return
	}

	data.Id = types.StringValue(role.Id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MssqlRoleResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	err := r.ctx.Client.DeleteRole(ctx, database, data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete role", fmt.Sprintf("Unable to delete role %s: %s", data.Id.ValueString(), err))
		return
	}
}

func (r *MssqlRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// ID format: database/rolename or just rolename (for provider's default database)
	parts := strings.SplitN(req.ID, "/", 2)

	if len(parts) == 2 {
		// database/rolename format
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
	} else {
		// Just rolename - uses provider's default database
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	}
}
