package core

import "github.com/vsabella/terraform-provider-mssql/internal/mssql"

type ProviderData struct {
	Client   mssql.SqlClient
	ServerID string
	Database string
	Enabled  bool
	// DisableReason provides actionable context when provider configuration is intentionally disabled.
	DisableReason string
}
