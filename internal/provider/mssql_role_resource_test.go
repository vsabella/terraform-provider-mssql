// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccMssqlRoleResource_InDefaultDatabase(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "mssql_role" "test" {
  name = "test_role_default"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_role.test", "name", "test_role_default"),
				),
			},
		},
	})
}

func TestAccMssqlRoleResource_InSpecificDatabase(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "mssql_database" "test" {
  name = "test_role_db"
}

resource "mssql_role" "test" {
  database = mssql_database.test.name
  name     = "app_readers"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_role.test", "name", "app_readers"),
					resource.TestCheckResourceAttr("mssql_role.test", "database", "test_role_db"),
				),
			},
		},
	})
}

func TestAccMssqlRoleAssignment_DatabaseRole(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "mssql_database" "test" {
  name = "test_role_assign_db"
}

resource "mssql_login" "test" {
  name     = "test_login_for_role"
  password = "TestPassword123!"
}

resource "mssql_user" "test" {
  database       = mssql_database.test.name
  username       = "test_user_for_role"
  login_name     = mssql_login.test.name
  default_schema = "dbo"
}

resource "mssql_role_assignment" "test" {
  database  = mssql_database.test.name
  role      = "db_datareader"
  principal = mssql_user.test.username
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_role_assignment.test", "role", "db_datareader"),
					resource.TestCheckResourceAttr("mssql_role_assignment.test", "principal", "test_user_for_role"),
					resource.TestCheckResourceAttr("mssql_role_assignment.test", "database", "test_role_assign_db"),
					resource.TestCheckResourceAttr("mssql_role_assignment.test", "server_role", "false"),
				),
			},
			{
				ResourceName:            "mssql_role_assignment.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateId:           "test_role_assign_db:db_datareader/test_user_for_role",
				ImportStateVerifyIgnore: []string{},
			},
		},
	})
}

func TestAccMssqlRoleAssignment_ServerRole(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "mssql_login" "test" {
  name     = "test_login_server_role"
  password = "TestPassword123!"
}

resource "mssql_role_assignment" "test" {
  server_role = true
  role        = "##MS_ServerStateReader##"
  principal   = mssql_login.test.name
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_role_assignment.test", "role", "##MS_ServerStateReader##"),
					resource.TestCheckResourceAttr("mssql_role_assignment.test", "principal", "test_login_server_role"),
					resource.TestCheckResourceAttr("mssql_role_assignment.test", "server_role", "true"),
				),
			},
			{
				ResourceName:            "mssql_role_assignment.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateId:           "server:##MS_ServerStateReader##/test_login_server_role",
				ImportStateVerifyIgnore: []string{},
			},
		},
	})
}
