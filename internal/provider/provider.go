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
)

// Ensure MssqlProvider satisfies various provider interfaces.
var _ provider.Provider = &MssqlProvider{}
var _ provider.ProviderWithFunctions = &MssqlProvider{}

// MssqlProvider defines the provider implementation.
type MssqlProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version      string
	providerData core.ProviderData
}

type SqlAuth struct {
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
}

//
//type azureAuth struct {
//	ClientId     types.String `tfsdk:"client_id"`
//	ClientSecret types.String `tfsdk:"client_secret"`
//	TenantId     types.String `tfsdk:"tenant_id"`
//}

type MssqlProviderModel struct {
	Host    types.String `tfsdk:"host"`
	Port    types.Int64  `tfsdk:"port"`
	SqlAuth *SqlAuth     `tfsdk:"sql_auth"`
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
			"Unknown MSSQL Server Host",
			"The provider needs the hostname or IP address of Microsoft SQL Server.",
		)
	}

	if data.Port.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Unknown MSSQL Server Port",
			"The provider needs the port of Microsoft SQL Server.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Create Client Context
	factory := core.NewMockClientFactory()
	client := &core.ProviderData{
		ClientFactory: func(ctx context.Context, db string) (core.SqlClient, error) {
			return factory.GetClient(db), nil
		},
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *MssqlProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewMssqlUserResource,
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
