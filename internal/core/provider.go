package core

import "github.com/vsabella/terraform-provider-mssql/internal/mssql"

type ProviderData struct {
	Client   mssql.SqlClient
	ServerID string
	Database string
}
