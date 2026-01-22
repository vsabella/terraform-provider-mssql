package provider

import (
	"context"
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
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &MssqlScriptResource{}
var _ resource.ResourceWithImportState = &MssqlScriptResource{}

func NewMssqlScriptResource() resource.Resource {
	return &MssqlScriptResource{}
}

type MssqlScriptResource struct {
	ctx core.ProviderData
}

type MssqlScriptResourceModel struct {
	Id           types.String `tfsdk:"id"`
	DatabaseName types.String `tfsdk:"database_name"`
	Name         types.String `tfsdk:"name"`
	CreateScript types.String `tfsdk:"create_script"`
	DeleteScript types.String `tfsdk:"delete_script"`
	Version      types.String `tfsdk:"version"`
}

func (r *MssqlScriptResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_script"
}

func (r *MssqlScriptResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `Manages arbitrary SQL scripts with Terraform lifecycle tracking.

Use this resource to install tools, run bootstrap scripts, or execute any SQL that needs to be managed as infrastructure.

The script is executed on create and when the version changes. Terraform tracks the version in state to determine when to re-run the script.

delete_script is only executed when the resource is destroyed (not when version changes).

**Example usage:**
` + "```hcl" + `
resource "mssql_script" "first_responder_kit" {
  database_name = "master"
  name          = "first_responder_kit"
  create_script = file("${path.module}/install-blitz.sql")
  delete_script = "DROP PROC sp_Blitz"
  version       = md5(file("${path.module}/install-blitz.sql"))
}
` + "```",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Resource identifier in format `<server_id>/<database>/<name>` where `server_id` is `host:port`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"database_name": schema.StringAttribute{
				MarkdownDescription: "Database where the script will be executed.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Logical name for the script (used for identification in state). Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"create_script": schema.StringAttribute{
				MarkdownDescription: "T-SQL script to execute on create and when version changes. Changing this without changing `version` will update state but will **not** re-run the script; bump `version` to re-execute.",
				Required:            true,
			},
			"delete_script": schema.StringAttribute{
				MarkdownDescription: "T-SQL script to execute on destroy. If not provided, no cleanup is performed.",
				Optional:            true,
			},
			"version": schema.StringAttribute{
				MarkdownDescription: "Version string to track script changes. When this changes, the create_script is re-executed in-place (no destroy/recreate). Typically set to `md5(file(\"script.sql\"))` to automatically detect file changes.",
				Required:            true,
			},
		},
	}
}

func (r *MssqlScriptResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *MssqlScriptResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MssqlScriptResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning(
		"Executing arbitrary SQL",
		"The mssql_script resource executes the provided SQL as-is. Review scripts carefully and ensure they are idempotent and safe.",
	)

	// Execute the create script
	if err := r.ctx.Client.ExecScript(ctx, data.DatabaseName.ValueString(), data.CreateScript.ValueString()); err != nil {
		resp.Diagnostics.AddError(
			fmt.Sprintf("Error executing script %s", data.Name.ValueString()),
			err.Error(),
		)
		return
	}

	data.Id = types.StringValue(fmt.Sprintf("%s/%s/%s", r.ctx.ServerID, data.DatabaseName.ValueString(), data.Name.ValueString()))
	tflog.Debug(ctx, fmt.Sprintf("Executed script %s in database %s", data.Name.ValueString(), data.DatabaseName.ValueString()))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlScriptResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MssqlScriptResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// We don't query the database to check if the script objects exist.
	// The resource is purely tracked via Terraform state and version.

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlScriptResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state MssqlScriptResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Re-execute on version change (in-place).
	if !plan.Version.Equal(state.Version) {
		resp.Diagnostics.AddWarning(
			"Executing arbitrary SQL",
			"The mssql_script resource executes the provided SQL as-is. Review scripts carefully and ensure they are idempotent and safe.",
		)

		if err := r.ctx.Client.ExecScript(ctx, plan.DatabaseName.ValueString(), plan.CreateScript.ValueString()); err != nil {
			resp.Diagnostics.AddError(
				fmt.Sprintf("Error executing script %s", plan.Name.ValueString()),
				err.Error(),
			)
			return
		}
		tflog.Debug(ctx, fmt.Sprintf("Re-executed script %s in database %s due to version change", plan.Name.ValueString(), plan.DatabaseName.ValueString()))
	}

	// If create_script changes without version change, we intentionally do NOT re-run.
	// Emit a warning to remind users to bump version to re-execute.
	if plan.Version.Equal(state.Version) && !plan.CreateScript.Equal(state.CreateScript) {
		resp.Diagnostics.AddWarning(
			"create_script changed without version bump",
			"Script will not be re-executed because version is unchanged. Bump version to re-run.",
		)
	}

	// Persist state (delete_script or other non-version/non-name changes).
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MssqlScriptResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MssqlScriptResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Execute delete script if provided
	if !data.DeleteScript.IsNull() && data.DeleteScript.ValueString() != "" {
		if err := r.ctx.Client.ExecScript(ctx, data.DatabaseName.ValueString(), data.DeleteScript.ValueString()); err != nil {
			// Log warning but don't fail - we still want to remove from state
			tflog.Warn(ctx, fmt.Sprintf("Error executing delete script for %s: %v", data.Name.ValueString(), err))
			resp.Diagnostics.AddWarning(
				"Delete script failed",
				fmt.Sprintf("The delete script for %s failed to execute: %v. The resource will be removed from state.", data.Name.ValueString(), err),
			)
		} else {
			tflog.Debug(ctx, fmt.Sprintf("Executed delete script for %s in database %s", data.Name.ValueString(), data.DatabaseName.ValueString()))
		}
	} else {
		tflog.Debug(ctx, fmt.Sprintf("No delete script provided for %s, skipping cleanup", data.Name.ValueString()))
	}
}

func (r *MssqlScriptResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import ID must be <server_id>/<database>/<name>
	database, name, err := parseScriptId(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database_name"), database)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/%s/%s", r.ctx.ServerID, database, name))...)
}

func parseScriptId(id string) (string, string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", fmt.Errorf("expected id in format <server_id>/<database>/<name>, got %q", id)
	}
	db, err := url.QueryUnescape(parts[1])
	if err != nil {
		return "", "", err
	}
	name, err := url.QueryUnescape(parts[2])
	if err != nil {
		return "", "", err
	}
	return db, name, nil
}
