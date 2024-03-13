// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/openaxon/terraform-provider-mssql/internal/core"
	"github.com/openaxon/terraform-provider-mssql/internal/mssql"
)

// Ensure MssqlProvider satisfies various provider interfaces.
var _ provider.Provider = &MssqlProvider{}
var _ provider.ProviderWithFunctions = &MssqlProvider{}

// MssqlProvider defines the provider implementation.
type MssqlProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

type SqlAuth struct {
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
}

type MssqlProviderModel struct {
	Host     types.String `tfsdk:"host"`
	Port     types.Int64  `tfsdk:"port"`
	Database types.String `tfsdk:"database"`
	SqlAuth  *SqlAuth     `tfsdk:"sql_auth"`
}

func (p *MssqlProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "mssql"
	resp.Version = p.version
}

func (p *MssqlProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				MarkdownDescription: "MSSQL Server Hostname",
				Required:            true,
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "MSSQL Server Port. Default: `1433`",
				Optional:            true,
			},
			"database": schema.StringAttribute{
				MarkdownDescription: "Database to connect to.",
				Required:            true,
			},
			"sql_auth": schema.SingleNestedAttribute{
				Description: "When provided, SQL authentication will be used when connecting.",
				Optional:    true,
				Attributes: map[string]schema.Attribute{
					"username": schema.StringAttribute{
						Description: "User name for SQL authentication.",
						Required:    true,
					},
					"password": schema.StringAttribute{
						Description: "Password for SQL authentication.",
						Required:    true,
						Sensitive:   true,
					},
				},
			},
		},
	}
}

func (p *MssqlProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data MssqlProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	// Configuration values are now available.
	// if data.Endpoint.IsNull() { /* ... */ }

	if data.Host.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Unknown Sql Server Host",
			"The provider needs the hostname or IP address of Microsoft SQL Server.",
		)
	}

	if data.Port.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Unknown Sql Server Server Port",
			"The provider needs the port of Microsoft SQL Server.",
		)
	}

	if data.Database.IsUnknown() || data.Database.IsNull() || data.Database.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Unknown Sql Server Database",
			"The provider is designed for Contained Databases / Azure SQL and will only connect to a single database at a time.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Create Client Context
	client := &core.ProviderData{
		Client: mssql.NewClient(data.Host.ValueString(), data.Port.ValueInt64(), data.Database.ValueString(), data.SqlAuth.Username.ValueString(), data.SqlAuth.Password.ValueString()),
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *MssqlProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewMssqlUserResource,
		NewMssqlRoleAssignmentResource,
	}
}

func (p *MssqlProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func (p *MssqlProvider) Functions(ctx context.Context) []func() function.Function {
	return []func() function.Function{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &MssqlProvider{
			version: version,
		}
	}
}
