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
}

func encodeGrantId(database, principal, permission string) string {
	// Store permission in lower-case for stability, but encode it to be safe in IDs.
	return fmt.Sprintf(
		"%s/%s/%s",
		url.QueryEscape(database),
		url.QueryEscape(principal),
		url.QueryEscape(strings.ToLower(permission)),
	)
}

func decodeGrantId(id string) (string, string, string, error) {
	dbEnc, rest, found := strings.Cut(id, "/")
	if !found {
		return "", "", "", fmt.Errorf("expected id in format <database>/<principal>/<permission>, got %q", id)
	}
	prEnc, permEnc, found := strings.Cut(rest, "/")
	if !found {
		return "", "", "", fmt.Errorf("expected id in format <database>/<principal>/<permission>, got %q", id)
	}

	db, err := url.QueryUnescape(dbEnc)
	if err != nil {
		return "", "", "", err
	}
	principal, err := url.QueryUnescape(prEnc)
	if err != nil {
		return "", "", "", err
	}
	perm, err := url.QueryUnescape(permEnc)
	if err != nil {
		return "", "", "", err
	}

	return db, principal, perm, nil
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
				MarkdownDescription: "`<database>/<principal>/<permission>`.",
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
				MarkdownDescription: "Name of database-level SQL permission. For full list of supported permissions, see [docs](https://learn.microsoft.com/en-us/sql/t-sql/statements/grant-database-permissions-transact-sql?view=azuresqldb-current#remarks)",
				Required:            true,
				Validators: []validator.String{
					databasePermissionValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"principal": schema.StringAttribute{
				MarkdownDescription: "Database principal to grant permission to.",
				Required:            true,
				Validators: []validator.String{
					principalNameValidator{},
				},
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

func (r *MssqlGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MssqlGrantResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := data.Database.ValueString()
	if data.Database.IsUnknown() || data.Database.IsNull() || database == "" {
		// Try decoding from ID first (for imports), otherwise fall back to provider default.
		if db, principal, perm, err := decodeGrantId(data.Id.ValueString()); err == nil {
			database = db
			data.Database = types.StringValue(database)
			data.Principal = types.StringValue(principal)
			data.Permission = types.StringValue(strings.ToUpper(perm))
		} else {
			database = r.ctx.Database
		}
	}

	// Client expects id format: <principal>/<permission>
	perm, err := r.ctx.Client.ReadDatabasePermission(ctx, database, fmt.Sprintf("%s/%s", data.Principal.ValueString(), strings.ToUpper(data.Permission.ValueString())))

	// If resource is not found, remove it from the state
	if errors.Is(err, sql.ErrNoRows) {
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable", fmt.Sprintf("Unable to read grant. Error: %s", err))
		return
	}

	data.Id = types.StringValue(encodeGrantId(database, perm.Principal, perm.Permission))
	data.Database = types.StringValue(database)
	data.Principal = types.StringValue(perm.Principal)
	data.Permission = types.StringValue(perm.Permission)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MssqlGrantResourceModel

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

	perm, err := r.ctx.Client.GrantDatabasePermission(ctx, database, data.Principal.ValueString(), strings.ToUpper(data.Permission.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error granting permission %s to principal %s", data.Permission.ValueString(), data.Principal.ValueString()), err.Error())
		return
	}

	data.Id = types.StringValue(encodeGrantId(database, perm.Principal, perm.Permission))
	tflog.Debug(ctx, fmt.Sprintf("Granted permssion %s to principal %s", data.Permission, data.Principal))

	// Save data into Terraform state
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
		// Try decode from ID for imports
		if db, _, _, err := decodeGrantId(data.Id.ValueString()); err == nil {
			database = db
		} else {
			database = r.ctx.Database
		}
	}

	err := r.ctx.Client.RevokeDatabasePermission(ctx, database, data.Principal.ValueString(), strings.ToUpper(data.Permission.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Unable to revoke permission", fmt.Sprintf("Unable to revoke permission %s from principal %s", data.Permission.ValueString(), data.Principal.ValueString()))
		return
	}
}

func (r *MssqlGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import formats:
	// - <principal>/<permission> (uses provider database)
	// - <database>/<principal>/<permission>
	parts := strings.Split(req.ID, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), r.ctx.Database)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("permission"), strings.ToUpper(parts[1]))...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), encodeGrantId(r.ctx.Database, parts[0], parts[1]))...)
		return
	}
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal"), parts[1])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("permission"), strings.ToUpper(parts[2]))...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), encodeGrantId(parts[0], parts[1], parts[2]))...)
		return
	}

	resp.Diagnostics.AddError("Invalid import ID", "expected <principal>/<permission> or <database>/<principal>/<permission>")
}
