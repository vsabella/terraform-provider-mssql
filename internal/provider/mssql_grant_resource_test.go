// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccMssqlGrantResource_DatabaseLevel(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create database-level grant
			{
				Config: providerConfig + testAccMssqlGrantDatabaseLevelConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_grant.create_proc", "permission", "CREATE PROCEDURE"),
					resource.TestCheckResourceAttr("mssql_grant.create_proc", "principal", "grant_test_user"),
				),
			},
		},
	})
}

func TestAccMssqlGrantResource_SchemaLevel(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create schema and grant CONTROL on it
			{
				Config: providerConfig + testAccMssqlGrantSchemaLevelConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_grant.schema_control", "permission", "CONTROL"),
					resource.TestCheckResourceAttr("mssql_grant.schema_control", "principal", "schema_grant_user"),
					resource.TestCheckResourceAttr("mssql_grant.schema_control", "object_type", "SCHEMA"),
					resource.TestCheckResourceAttr("mssql_grant.schema_control", "object_name", "tools"),
				),
			},
		},
	})
}

func TestAccMssqlGrantResource_SchemaQualifiedObject(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccMssqlGrantSchemaQualifiedObjectConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_grant.table_select", "permission", "SELECT"),
					resource.TestCheckResourceAttr("mssql_grant.table_select", "principal", "schema_object_user"),
					// Expect TABLE (preserve user-specified type even though server stores class=OBJECT)
					resource.TestCheckResourceAttr("mssql_grant.table_select", "object_type", "TABLE"),
					resource.TestCheckResourceAttr("mssql_grant.table_select", "object_name", "tools.widgets"),
				),
			},
			// Re-apply to ensure no drift
			{
				Config: providerConfig + testAccMssqlGrantSchemaQualifiedObjectConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_grant.table_select", "object_type", "TABLE"),
					resource.TestCheckResourceAttr("mssql_grant.table_select", "object_name", "tools.widgets"),
				),
			},
			// Import and verify object_type stays TABLE (no normalization drift)
			{
				ResourceName:      "mssql_grant.table_select",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					db := s.RootModule().Resources["mssql_database.gdb"].Primary.Attributes["name"]
					principal := s.RootModule().Resources["mssql_user.schema_object_user"].Primary.Attributes["username"]
					return fmt.Sprintf("127.0.0.1:1433/%s/%s/SELECT/TABLE/tools.widgets", db, principal), nil
				},
			},
		},
	})
}

func testAccMssqlGrantDatabaseLevelConfig() string {
	return `
resource "mssql_user" "grant_test" {
  username = "grant_test_user"
  password = "GrantTestPassword123!@#"
}

resource "mssql_grant" "create_proc" {
  permission = "CREATE PROCEDURE"
  principal  = mssql_user.grant_test.username
}
`
}

func testAccMssqlGrantSchemaLevelConfig() string {
	return `
resource "mssql_user" "schema_grant_user" {
  database = "testdb"
  username = "schema_grant_user"
  password = "SchemaGrantPassword123!@#"
}

resource "mssql_script" "tools_schema" {
  database_name = "testdb"
  name          = "tools_schema"
  create_script = "IF NOT EXISTS (SELECT * FROM sys.schemas WHERE name = 'tools') EXEC('CREATE SCHEMA [tools] AUTHORIZATION [dbo]')"
  delete_script = "DROP SCHEMA IF EXISTS [tools]"
  version       = "v1"
}

resource "mssql_grant" "schema_control" {
  database    = "testdb"
  permission  = "CONTROL"
  principal   = mssql_user.schema_grant_user.username
  object_type = "SCHEMA"
  object_name = "tools"

  depends_on = [mssql_script.tools_schema]
}
`
}

func testAccMssqlGrantSchemaQualifiedObjectConfig() string {
	return `
resource "mssql_database" "gdb" {
  name = "testdb_schema_obj"
}

resource "mssql_login" "schema_object_login" {
  name     = "schema_object_login"
  password = "SchemaObjLogin123!@#"
}

resource "mssql_user" "schema_object_user" {
  database = mssql_database.gdb.name
  username = "schema_object_user"
  login_name = mssql_login.schema_object_login.name
  default_schema = "dbo"
}

resource "mssql_script" "tools_schema" {
  database_name = mssql_database.gdb.name
  name          = "tools_schema"
  create_script = <<-SQL
    IF NOT EXISTS (SELECT * FROM sys.schemas WHERE name = 'tools') EXEC('CREATE SCHEMA [tools] AUTHORIZATION [dbo]');
    IF OBJECT_ID('[tools].[widgets]', 'U') IS NULL
    BEGIN
      CREATE TABLE [tools].[widgets](id int PRIMARY KEY);
    END
  SQL
  delete_script = "DROP TABLE IF EXISTS [tools].[widgets]; DROP SCHEMA IF EXISTS [tools];"
  version       = "v1"
}

resource "mssql_grant" "table_select" {
  database    = mssql_database.gdb.name
  permission  = "SELECT"
  principal   = mssql_user.schema_object_user.username
  object_type = "TABLE"
  object_name = "tools.widgets"

  depends_on = [mssql_script.tools_schema]
}
`
}
