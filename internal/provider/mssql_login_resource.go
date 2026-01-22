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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/vsabella/terraform-provider-mssql/internal/core"
	"github.com/vsabella/terraform-provider-mssql/internal/mssql"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &MssqlLoginResource{}
var _ resource.ResourceWithImportState = &MssqlLoginResource{}

func NewMssqlLoginResource() resource.Resource {
	return &MssqlLoginResource{}
}

type MssqlLoginResource struct {
	ctx core.ProviderData
}

type MssqlLoginResourceModel struct {
	Id              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Password        types.String `tfsdk:"password"`
	DefaultDatabase types.String `tfsdk:"default_database"`
	DefaultLanguage types.String `tfsdk:"default_language"`
	Sid             types.String `tfsdk:"sid"`
	AutoImport      types.Bool   `tfsdk:"auto_import"`
}

func (r *MssqlLoginResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_login"
}

func (r *MssqlLoginResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a SQL Server login (server-level principal). Use this resource to create SQL authentication logins that can then be mapped to database users.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Resource identifier in format `<server_id>/<login_name>` where `server_id` is `host:port`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Login name. Changing this forces a new resource to be created.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "Password for the login.\n\n" +
					"~> **Note** Password will be stored in the raw state as plain-text. [Read more about sensitive data in state](https://www.terraform.io/language/state/sensitive-data).",
				Required:  true,
				Sensitive: true,
			},
			"default_database": schema.StringAttribute{
				MarkdownDescription: "Default database for the login. Defaults to `master`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("master"),
			},
			"default_language": schema.StringAttribute{
				MarkdownDescription: "Default language for the login. If not specified, uses the server default.",
				Optional:            true,
				Computed:            true,
			},
			"sid": schema.StringAttribute{
				MarkdownDescription: "SID for the login, as a hex string (e.g., `0x010500000000000515000000...`). Changing this forces a new resource to be created.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"auto_import": schema.BoolAttribute{
				MarkdownDescription: "If true, and the login already exists, adopt it into state instead of failing create. Existing logins are not modified during adoption.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
		},
	}
}

func (r *MssqlLoginResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *MssqlLoginResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MssqlLoginResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	create := mssql.CreateLogin{
		Name:            data.Name.ValueString(),
		Password:        data.Password.ValueString(),
		DefaultDatabase: data.DefaultDatabase.ValueString(),
		DefaultLanguage: data.DefaultLanguage.ValueString(),
		Sid:             data.Sid.ValueString(),
	}

	if data.AutoImport.ValueBool() {
		login, err := r.ctx.Client.GetLogin(ctx, create.Name)
		if err == nil {
			if !data.Sid.IsNull() && !data.Sid.IsUnknown() && data.Sid.ValueString() != "" &&
				login.Sid != "" && !strings.EqualFold(data.Sid.ValueString(), login.Sid) {
				resp.Diagnostics.AddError(
					"Existing login SID mismatch",
					fmt.Sprintf("Login %s already exists with SID %s, which does not match the configured SID %s.",
						create.Name, login.Sid, data.Sid.ValueString()),
				)
				return
			}

			loginToResourceWithServer(&data, login, r.ctx.ServerID)
			tflog.Debug(ctx, fmt.Sprintf("Adopted existing login %s", data.Name.ValueString()))
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			resp.Diagnostics.AddError(fmt.Sprintf("Error checking login %s", create.Name), err.Error())
			return
		}
	}

	login, err := r.ctx.Client.CreateLogin(ctx, create)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error creating login %s", create.Name), err.Error())
		return
	}

	loginToResourceWithServer(&data, login, r.ctx.ServerID)

	tflog.Debug(ctx, fmt.Sprintf("Created login %s", data.Name.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func loginToResourceWithServer(data *MssqlLoginResourceModel, login mssql.Login, serverID string) {
	data.Id = types.StringValue(fmt.Sprintf("%s/%s", serverID, login.Name))
	data.Name = types.StringValue(login.Name)
	data.DefaultDatabase = types.StringValue(login.DefaultDatabase)
	if login.DefaultLanguage != "" {
		data.DefaultLanguage = types.StringValue(login.DefaultLanguage)
	}
	if login.Sid != "" {
		data.Sid = types.StringValue(login.Sid)
	} else {
		data.Sid = types.StringNull()
	}
}

func parseLoginId(id string) (string, error) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("expected id in format <server_id>/<login_name>, got %q", id)
	}
	loginName, err := url.QueryUnescape(parts[1])
	if err != nil {
		return "", err
	}
	return loginName, nil
}

func (r *MssqlLoginResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MssqlLoginResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	loginName, err := parseLoginId(data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid login ID", err.Error())
		return
	}
	login, err := r.ctx.Client.GetLogin(ctx, loginName)

	if errors.Is(err, sql.ErrNoRows) {
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable to read login", fmt.Sprintf("Unable to read login %s, got error: %s", loginName, err))
		return
	}

	// Preserve password from state (cannot be read from the server).
	loginToResourceWithServer(&data, login, r.ctx.ServerID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlLoginResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data MssqlLoginResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	update := mssql.UpdateLogin{
		Name:            data.Name.ValueString(),
		Password:        data.Password.ValueString(),
		DefaultDatabase: data.DefaultDatabase.ValueString(),
		DefaultLanguage: data.DefaultLanguage.ValueString(),
	}

	login, err := r.ctx.Client.UpdateLogin(ctx, update)
	if err != nil {
		resp.Diagnostics.AddError("Could not update login", err.Error())
		return
	}

	// Preserve password from plan/state (cannot be read from the server).
	loginToResourceWithServer(&data, login, r.ctx.ServerID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlLoginResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MssqlLoginResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	loginName, err := parseLoginId(data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid login ID", err.Error())
		return
	}
	if err := r.ctx.Client.DeleteLogin(ctx, loginName); err != nil {
		resp.Diagnostics.AddError("Unable to delete login", fmt.Sprintf("Unable to delete login %s, got error: %s", data.Name.ValueString(), err))
		return
	}
}

func (r *MssqlLoginResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import ID: <server_id>/<login_name>
	loginName, err := parseLoginId(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	login, err := r.ctx.Client.GetLogin(ctx, loginName)
	if err != nil {
		resp.Diagnostics.AddError("Unable to import login", fmt.Sprintf("Login %s not found or error occurred: %s", loginName, err))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/%s", r.ctx.ServerID, login.Name))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), login.Name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("default_database"), login.DefaultDatabase)...)
	if login.DefaultLanguage != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("default_language"), login.DefaultLanguage)...)
	}
	if login.Sid != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("sid"), login.Sid)...)
	}

	// Password cannot be imported - user will need to set it
	resp.Diagnostics.AddWarning(
		"Password not imported",
		"The login password cannot be read from the server. You must set the password attribute in your configuration. The next apply will update the password.",
	)
}
