package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/vsabella/terraform-provider-mssql/internal/core"
)

func configureResourceProviderData(resourceName string, providerData interface{}, diags *diag.Diagnostics) (core.ProviderData, bool) {
	if providerData == nil {
		return core.ProviderData{
			Enabled:       false,
			DisableReason: fmt.Sprintf("%s provider is not configured yet.", resourceName),
		}, true
	}

	client, ok := providerData.(*core.ProviderData)
	if !ok {
		diags.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *core.ProviderData, got: %T. Please report this issue to the provider developers.", providerData),
		)
		return core.ProviderData{}, false
	}

	return *client, true
}

func ensureProviderReady(resourceName string, ctx core.ProviderData, diags *diag.Diagnostics) bool {
	if !ctx.Enabled {
		reason := strings.TrimSpace(ctx.DisableReason)
		if reason == "" {
			reason = "This provider alias is disabled."
		}
		diags.AddError(
			"Provider alias disabled",
			fmt.Sprintf("%s cannot operate because its provider alias is disabled. %s", resourceName, reason),
		)
		return false
	}

	if ctx.Client == nil {
		diags.AddError(
			"Provider client unavailable",
			fmt.Sprintf("%s cannot operate because the provider client is not initialized. Verify host, sql_auth, and provider alias configuration.", resourceName),
		)
		return false
	}

	if strings.TrimSpace(ctx.ServerID) == "" {
		diags.AddError(
			"Provider server ID unavailable",
			fmt.Sprintf("%s cannot operate because the provider server identifier is empty. Verify provider host and port configuration.", resourceName),
		)
		return false
	}

	return true
}

func ensureServerIDMatch(resourceName, idServerID, providerServerID string, diags *diag.Diagnostics) bool {
	parsedServerID := strings.TrimSpace(idServerID)
	configuredServerID := strings.TrimSpace(providerServerID)

	if parsedServerID == "" {
		diags.AddError(
			"Invalid resource server ID",
			fmt.Sprintf("%s state has an empty server_id. Re-import or repair state for this resource before apply/destroy.", resourceName),
		)
		return false
	}
	if configuredServerID == "" {
		diags.AddError(
			"Provider/server mismatch",
			fmt.Sprintf("%s state targets server %q, but provider server_id is empty. Reconfigure this provider alias to %q and retry.", resourceName, parsedServerID, parsedServerID),
		)
		return false
	}
	if !strings.EqualFold(parsedServerID, configuredServerID) {
		diags.AddError(
			"Provider/server mismatch",
			fmt.Sprintf("%s state targets server %q, but provider is configured for %q. Reconfigure this provider alias to %q before apply/destroy.", resourceName, parsedServerID, configuredServerID, parsedServerID),
		)
		return false
	}
	return true
}
