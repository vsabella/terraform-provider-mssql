package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/vsabella/terraform-provider-mssql/internal/core"
)

func TestEnsureProviderReady_Disabled(t *testing.T) {
	var diags diag.Diagnostics

	ok := ensureProviderReady("mssql_login", core.ProviderData{
		Enabled:       false,
		DisableReason: "disabled for teardown",
	}, &diags)

	if ok {
		t.Fatalf("expected provider readiness check to fail")
	}
	if !diags.HasError() {
		t.Fatalf("expected diagnostics to include an error")
	}
}

func TestEnsureProviderReady_MissingClient(t *testing.T) {
	var diags diag.Diagnostics

	ok := ensureProviderReady("mssql_user", core.ProviderData{
		Enabled:  true,
		ServerID: "server.example:1433",
	}, &diags)

	if ok {
		t.Fatalf("expected provider readiness check to fail for missing client")
	}
	if !diags.HasError() {
		t.Fatalf("expected diagnostics to include an error")
	}
}

func TestEnsureServerIDMatch(t *testing.T) {
	tests := []struct {
		name             string
		idServerID       string
		providerServerID string
		expectOK         bool
	}{
		{
			name:             "exact match",
			idServerID:       "server.example:1433",
			providerServerID: "server.example:1433",
			expectOK:         true,
		},
		{
			name:             "case insensitive match",
			idServerID:       "Server.Example:1433",
			providerServerID: "server.example:1433",
			expectOK:         true,
		},
		{
			name:             "mismatch",
			idServerID:       "server-a:1433",
			providerServerID: "server-b:1433",
			expectOK:         false,
		},
		{
			name:             "empty id server id",
			idServerID:       "",
			providerServerID: "server-b:1433",
			expectOK:         false,
		},
		{
			name:             "whitespace id server id",
			idServerID:       "   ",
			providerServerID: "server-b:1433",
			expectOK:         false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics

			ok := ensureServerIDMatch("mssql_script", tc.idServerID, tc.providerServerID, &diags)
			if ok != tc.expectOK {
				t.Fatalf("expected %v, got %v", tc.expectOK, ok)
			}
			if tc.expectOK && diags.HasError() {
				t.Fatalf("did not expect diagnostics error")
			}
			if !tc.expectOK && !diags.HasError() {
				t.Fatalf("expected diagnostics error")
			}
		})
	}
}

func TestConfigureResourceProviderData_Nil(t *testing.T) {
	var diags diag.Diagnostics

	ctx, ok := configureResourceProviderData("mssql_role", nil, &diags)
	if !ok {
		t.Fatalf("expected configureResourceProviderData to succeed with disabled context")
	}
	if diags.HasError() {
		t.Fatalf("did not expect diagnostics error")
	}
	if ctx.Enabled {
		t.Fatalf("expected disabled provider context when provider data is nil")
	}
}

func TestConfigureResourceProviderData_Valid(t *testing.T) {
	var diags diag.Diagnostics

	input := &core.ProviderData{
		Enabled:  true,
		ServerID: "server.example:1433",
	}

	ctx, ok := configureResourceProviderData("mssql_role", input, &diags)
	if !ok {
		t.Fatalf("expected configureResourceProviderData to succeed")
	}
	if diags.HasError() {
		t.Fatalf("did not expect diagnostics error")
	}
	if !ctx.Enabled {
		t.Fatalf("expected enabled provider context")
	}
	if ctx.ServerID != input.ServerID {
		t.Fatalf("expected server id %q, got %q", input.ServerID, ctx.ServerID)
	}
}

func TestConfigureResourceProviderData_WrongType(t *testing.T) {
	var diags diag.Diagnostics

	_, ok := configureResourceProviderData("mssql_role", "not-provider-data", &diags)
	if ok {
		t.Fatalf("expected configureResourceProviderData to fail for wrong type")
	}
	if !diags.HasError() {
		t.Fatalf("expected diagnostics error for wrong provider data type")
	}
}
