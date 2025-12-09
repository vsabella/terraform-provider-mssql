// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccMssqlScriptResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + testAccMssqlScriptResourceConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_script.test", "id", "testdb/test_script"),
					resource.TestCheckResourceAttr("mssql_script.test", "database_name", "testdb"),
					resource.TestCheckResourceAttr("mssql_script.test", "name", "test_script"),
					resource.TestCheckResourceAttr("mssql_script.test", "version", "v1"),
				),
			},
		},
	})
}

func TestAccMssqlScriptResource_VersionChange(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with version v1
			{
				Config: providerConfig + testAccMssqlScriptResourceConfigVersioned("v1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_script.versioned", "version", "v1"),
				),
			},
			// Update to version v2 - should trigger replace
			{
				Config: providerConfig + testAccMssqlScriptResourceConfigVersioned("v2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_script.versioned", "version", "v2"),
				),
			},
		},
	})
}

func TestAccMssqlScriptResource_WithDeleteScript(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with delete script
			{
				Config: providerConfig + testAccMssqlScriptResourceConfigWithDelete(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_script.with_delete", "name", "proc_with_delete"),
				),
			},
			// Destroy will execute delete_script
		},
	})
}

func testAccMssqlScriptResourceConfig() string {
	return `
resource "mssql_script" "test" {
  database_name = "testdb"
  name          = "test_script"
  create_script = "SELECT 1"
  version       = "v1"
}
`
}

func testAccMssqlScriptResourceConfigVersioned(version string) string {
	return `
resource "mssql_script" "versioned" {
  database_name = "testdb"
  name          = "versioned_script"
  create_script = "SELECT 1 AS version_check"
  version       = "` + version + `"
}
`
}

func testAccMssqlScriptResourceConfigWithDelete() string {
	return `
resource "mssql_script" "with_delete" {
  database_name = "testdb"
  name          = "proc_with_delete"
  create_script = <<-SQL
    IF OBJECT_ID('dbo.test_proc', 'P') IS NOT NULL
      DROP PROCEDURE dbo.test_proc;
    GO
    CREATE PROCEDURE dbo.test_proc
    AS
    BEGIN
      SELECT 1
    END
  SQL
  delete_script = "DROP PROCEDURE IF EXISTS dbo.test_proc"
  version       = "v1"
}
`
}










