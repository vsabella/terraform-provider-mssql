// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccMssqlLoginResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + testAccMssqlLoginResourceConfig("test_login", "TestPassword123!@#"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_login.test", "id", "test_login"),
					resource.TestCheckResourceAttr("mssql_login.test", "name", "test_login"),
					resource.TestCheckResourceAttr("mssql_login.test", "default_database", "master"),
				),
			},
			// Update password and default database
			{
				Config: providerConfig + testAccMssqlLoginResourceConfigWithDB("test_login", "NewPassword456!@#", "testdb"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_login.test", "name", "test_login"),
					resource.TestCheckResourceAttr("mssql_login.test", "default_database", "testdb"),
				),
			},
			// ImportState testing
			{
				ResourceName:            "mssql_login.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"}, // Password cannot be read back
				ImportStateId:           "test_login",
			},
		},
	})
}

func TestAccMssqlLoginResource_WithUser(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create login and user mapped to login
			{
				Config: providerConfig + testAccMssqlLoginAndUserResourceConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_login.app_login", "name", "app_login"),
					resource.TestCheckResourceAttr("mssql_user.app_user", "username", "app_user"),
					resource.TestCheckResourceAttr("mssql_user.app_user", "login_name", "app_login"),
				),
			},
		},
	})
}

func testAccMssqlLoginResourceConfig(name, password string) string {
	return fmt.Sprintf(`
resource "mssql_login" "test" {
  name     = %q
  password = %q
}
`, name, password)
}

func testAccMssqlLoginResourceConfigWithDB(name, password, defaultDB string) string {
	return fmt.Sprintf(`
resource "mssql_login" "test" {
  name             = %q
  password         = %q
  default_database = %q
}
`, name, password, defaultDB)
}

func testAccMssqlLoginAndUserResourceConfig() string {
	return `
resource "mssql_login" "app_login" {
  name     = "app_login"
  password = "AppPassword123!@#"
}

resource "mssql_user" "app_user" {
  username   = "app_user"
  login_name = mssql_login.app_login.name
}
`
}

func TestAccMssqlLoginResource_WithServerRole(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create login and assign to server role
			{
				Config: providerConfig + testAccMssqlLoginWithServerRoleConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_login.telemetry", "name", "telemetry_login"),
					resource.TestCheckResourceAttr("mssql_role_assignment.server_state_reader", "server_role", "true"),
					resource.TestCheckResourceAttr("mssql_role_assignment.server_state_reader", "role", "##MS_ServerStateReader##"),
					resource.TestCheckResourceAttr("mssql_role_assignment.server_state_reader", "principal", "telemetry_login"),
				),
			},
		},
	})
}

func testAccMssqlLoginWithServerRoleConfig() string {
	return `
resource "mssql_login" "telemetry" {
  name     = "telemetry_login"
  password = "TelemetryPassword123!@#"
}

resource "mssql_role_assignment" "server_state_reader" {
  server_role = true
  role        = "##MS_ServerStateReader##"
  principal   = mssql_login.telemetry.name
}
`
}
