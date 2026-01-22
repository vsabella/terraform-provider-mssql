package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

type principalNameValidator struct{}

func (v principalNameValidator) Description(ctx context.Context) string {
	return "Validates that a SQL principal name is non-empty and <= 128 characters."
}

func (v principalNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v principalNameValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
		return
	}

	raw := req.ConfigValue.ValueString()
	val := strings.TrimSpace(raw)
	if val == "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid principal", "principal must not be empty")
		return
	}
	if len(val) > 128 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid principal", "principal must be <= 128 characters")
		return
	}
}

type databasePermissionValidator struct{}

func (v databasePermissionValidator) Description(ctx context.Context) string {
	return "Validates that permission is composed of safe SQL tokens (letters/spaces/underscores)."
}

func (v databasePermissionValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v databasePermissionValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
		return
	}

	raw := req.ConfigValue.ValueString()
	val := strings.ToUpper(strings.TrimSpace(raw))
	if val == "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid permission", "permission must not be empty")
		return
	}
	for _, r := range val {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ' ' {
			continue
		}
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid permission",
			fmt.Sprintf("permission must contain only letters, digits, spaces, and underscores; got %q", raw),
		)
		return
	}
}

type objectTypeValidator struct{}

func (v objectTypeValidator) Description(ctx context.Context) string {
	return "Validates that object_type is one of SCHEMA, OBJECT, TABLE, VIEW, PROCEDURE, FUNCTION, or PROC."
}

func (v objectTypeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v objectTypeValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
		return
	}

	raw := strings.ToUpper(strings.TrimSpace(req.ConfigValue.ValueString()))
	if raw == "" {
		return
	}

	switch raw {
	case "SCHEMA", "OBJECT", "TABLE", "VIEW", "PROCEDURE", "FUNCTION", "PROC":
		return
	default:
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid object type",
			fmt.Sprintf("object_type must be one of SCHEMA, OBJECT, TABLE, VIEW, PROCEDURE, FUNCTION, or PROC; got %q", req.ConfigValue.ValueString()),
		)
	}
}
