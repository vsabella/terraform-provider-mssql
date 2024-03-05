package core

import "github.com/openaxon/terraform-provider-mssql/internal/mssql"

type ProviderData struct {
	Client mssql.SqlClient
}
