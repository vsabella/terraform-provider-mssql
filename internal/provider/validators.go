package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// objectTypeValidator validates mssql_grant.object_type to prevent invalid SQL tokens being
// interpolated into dynamic SQL.
//
// Allowed values (case-insensitive): SCHEMA, OBJECT, TABLE, VIEW, PROCEDURE, FUNCTION (and PROC).
// Note: TABLE/VIEW/PROCEDURE/FUNCTION are treated as OBJECT securables by SQL Server.
type objectTypeValidator struct{}

func (v objectTypeValidator) Description(ctx context.Context) string {
	return "Restricts object_type to a known allowlist (SCHEMA, OBJECT, TABLE, VIEW, PROCEDURE, FUNCTION, PROC)."
}

func (v objectTypeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v objectTypeValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
		return
	}

	raw := req.ConfigValue.ValueString()
	val := strings.ToUpper(strings.TrimSpace(raw))

	switch val {
	case "SCHEMA", "OBJECT", "TABLE", "VIEW", "PROCEDURE", "FUNCTION", "PROC":
		return
	default:
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid object_type",
			fmt.Sprintf("object_type must be one of SCHEMA, OBJECT, TABLE, VIEW, PROCEDURE, FUNCTION (or PROC); got %q", raw),
		)
	}
}
