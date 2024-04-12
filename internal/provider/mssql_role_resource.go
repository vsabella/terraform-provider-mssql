package provider

import (
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


type MssqlRoleResource struct {
	ctx core.ProviderData
}

type MssqlRoleResourceModel struct {
	Id        types.String `tfsdk:"id"`
}

func (r *MssqlRoleResourceModel) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *MssqlRoleResourceModel) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "MssqlRole resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
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

func (r *MssqlRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MssqlRoleResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

  role, err := r.ctx.Client.CreateRole(ctx, data.Role.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error creating role %s", data.Role.ValueString(), err.Error())
		return
	}

	data.Id = types.StringValue(role.Id)
	tflog.Debug(ctx, fmt.Sprintf("Created role %s", data.Role))

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}


