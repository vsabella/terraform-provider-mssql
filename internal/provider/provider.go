// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/vsabella/terraform-provider-mssql/internal/core"
	"github.com/vsabella/terraform-provider-mssql/internal/mssql"
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
				MarkdownDescription: "Database to connect to. Default: `master`",
				Optional:            true,
			},
			"sql_auth": schema.SingleNestedAttribute{
				Description: "SQL authentication credentials used when connecting.",
				Required:    true,
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
	} else if data.Host.IsNull() || strings.TrimSpace(data.Host.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing Sql Server Host",
			"`host` must be set to the hostname or IP address of Microsoft SQL Server.",
		)
	}

	if data.Port.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("port"),
			"Unknown Sql Server Port",
			"The provider needs the port of Microsoft SQL Server.",
		)
	}

	if data.Database.IsUnknown() || data.Database.IsNull() || data.Database.ValueString() == "" {
		resp.Diagnostics.AddWarning(
			"Unknown Sql Server Database, defaults to 'master'",
			"The provider is designed for Contained Databases and will only connect to a single database at a time. If not provided, the provider will default to 'master'.",
		)
		data.Database = types.StringValue("master")
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Validate sql_auth (required to connect)
	if data.SqlAuth == nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("sql_auth"),
			"Missing SQL Authentication",
			"`sql_auth` is required. Provide `sql_auth { username = \"...\" password = \"...\" }`.",
		)
		return
	}
	if data.SqlAuth.Username.IsUnknown() || data.SqlAuth.Username.IsNull() || data.SqlAuth.Username.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("sql_auth").AtName("username"),
			"Missing SQL Username",
			"`sql_auth.username` must be set.",
		)
	}
	if data.SqlAuth.Password.IsUnknown() || data.SqlAuth.Password.IsNull() || data.SqlAuth.Password.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("sql_auth").AtName("password"),
			"Missing SQL Password",
			"`sql_auth.password` must be set.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// Create Client Context
	host := data.Host.ValueString()
	port := data.Port.ValueInt64()
	if data.Port.IsNull() || port <= 0 {
		port = 1433
	}
	client := &core.ProviderData{
		Client:   mssql.NewClient(host, port, data.Database.ValueString(), data.SqlAuth.Username.ValueString(), data.SqlAuth.Password.ValueString()),
		Database: data.Database.ValueString(),
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *MssqlProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewMssqlUserResource,
		NewMssqlRoleResource,
		NewMssqlRoleAssignmentResource,
		NewMssqlGrantResource,
		NewMssqlDatabaseResource,
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
