// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccMssqlDatabaseResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + testAccMssqlDatabaseResourceConfig("test_database"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_database.test", "id", "127.0.0.1:1433/test_database"),
					resource.TestCheckResourceAttr("mssql_database.test", "name", "test_database"),
					// Verify default values
					resource.TestCheckResourceAttr("mssql_database.test", "recovery_model", "FULL"),
					resource.TestCheckResourceAttr("mssql_database.test", "read_committed_snapshot", "false"),
					resource.TestCheckResourceAttr("mssql_database.test", "auto_create_stats", "true"),
				),
			},
			// ImportState testing - import by database name
			{
				ResourceName:      "mssql_database.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(_ *terraform.State) (string, error) {
					return "127.0.0.1:1433/test_database", nil
				},
				// scoped_configuration is not imported
				ImportStateVerifyIgnore: []string{"scoped_configuration"},
			},
		},
	})
}

func TestAccMssqlDatabaseResource_WithOptions(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with options
			{
				Config: providerConfig + testAccMssqlDatabaseResourceConfigWithOptions("test_db_options"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_database.test", "name", "test_db_options"),
					resource.TestCheckResourceAttr("mssql_database.test", "recovery_model", "SIMPLE"),
					resource.TestCheckResourceAttr("mssql_database.test", "read_committed_snapshot", "true"),
					resource.TestCheckResourceAttr("mssql_database.test", "allow_snapshot_isolation", "true"),
					resource.TestCheckResourceAttr("mssql_database.test", "auto_close", "false"),
					resource.TestCheckResourceAttr("mssql_database.test", "auto_shrink", "false"),
				),
			},
			// Update options - toggle them off
			{
				Config: providerConfig + testAccMssqlDatabaseResourceConfigUpdatedOptions("test_db_options"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_database.test", "name", "test_db_options"),
					resource.TestCheckResourceAttr("mssql_database.test", "recovery_model", "FULL"),
					resource.TestCheckResourceAttr("mssql_database.test", "read_committed_snapshot", "false"),
					resource.TestCheckResourceAttr("mssql_database.test", "allow_snapshot_isolation", "false"),
				),
			},
		},
	})
}

func TestAccMssqlDatabaseResource_CompatibilityLevel(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with compatibility level 150 (SQL Server 2019)
			{
				Config: providerConfig + testAccMssqlDatabaseResourceConfigCompatLevel("test_db_compat", 150),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_database.test", "name", "test_db_compat"),
					resource.TestCheckResourceAttr("mssql_database.test", "compatibility_level", "150"),
				),
			},
			// Update to compatibility level 160 (SQL Server 2022)
			{
				Config: providerConfig + testAccMssqlDatabaseResourceConfigCompatLevel("test_db_compat", 160),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_database.test", "name", "test_db_compat"),
					resource.TestCheckResourceAttr("mssql_database.test", "compatibility_level", "160"),
				),
			},
		},
	})
}

func TestAccMssqlDatabaseResource_Collation(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccMssqlDatabaseResourceConfigCollation("test_db_collation", "SQL_Latin1_General_CP1_CI_AS"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_database.test", "name", "test_db_collation"),
					resource.TestCheckResourceAttr("mssql_database.test", "collation", "SQL_Latin1_General_CP1_CI_AS"),
				),
			},
		},
	})
}

func TestAccMssqlDatabaseResource_WithScopedConfig(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with scoped configurations
			{
				Config: providerConfig + testAccMssqlDatabaseResourceConfigWithScopedConfig("test_db_scoped"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_database.test", "name", "test_db_scoped"),
				),
			},
		},
	})
}

func TestAccMssqlDatabaseResource_MultipleDBs(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create multiple databases
			{
				Config: providerConfig + testAccMssqlDatabaseResourceConfigMultiple(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_database.db1", "id", "127.0.0.1:1433/test_db_one"),
					resource.TestCheckResourceAttr("mssql_database.db1", "name", "test_db_one"),
					resource.TestCheckResourceAttr("mssql_database.db2", "id", "127.0.0.1:1433/test_db_two"),
					resource.TestCheckResourceAttr("mssql_database.db2", "name", "test_db_two"),
				),
			},
		},
	})
}

func testAccMssqlDatabaseResourceConfig(name string) string {
	return fmt.Sprintf(`
resource "mssql_database" "test" {
  name = %q
}
`, name)
}

func testAccMssqlDatabaseResourceConfigWithOptions(name string) string {
	return fmt.Sprintf(`
resource "mssql_database" "test" {
  name = %q

  recovery_model           = "SIMPLE"
  read_committed_snapshot  = true
  allow_snapshot_isolation = true
  auto_close               = false
  auto_shrink              = false
  auto_create_stats        = true
  auto_update_stats        = true
  auto_update_stats_async  = false
}
`, name)
}

func testAccMssqlDatabaseResourceConfigUpdatedOptions(name string) string {
	return fmt.Sprintf(`
resource "mssql_database" "test" {
  name = %q

  recovery_model           = "FULL"
  read_committed_snapshot  = false
  allow_snapshot_isolation = false
  auto_close               = false
  auto_shrink              = false
  auto_create_stats        = true
  auto_update_stats        = true
  auto_update_stats_async  = false
}
`, name)
}

func testAccMssqlDatabaseResourceConfigWithScopedConfig(name string) string {
	return fmt.Sprintf(`
resource "mssql_database" "test" {
  name = %q

  scoped_configuration {
    name  = "MAXDOP"
    value = "8"
  }

  scoped_configuration {
    name  = "LEGACY_CARDINALITY_ESTIMATION"
    value = "OFF"
  }
}
`, name)
}

func testAccMssqlDatabaseResourceConfigMultiple() string {
	return `
resource "mssql_database" "db1" {
  name = "test_db_one"
}

resource "mssql_database" "db2" {
  name = "test_db_two"
}
`
}

func testAccMssqlDatabaseResourceConfigCollation(name string, collation string) string {
	return fmt.Sprintf(`
resource "mssql_database" "test" {
  name      = %q
  collation = %q
}
`, name, collation)
}

func testAccMssqlDatabaseResourceConfigCompatLevel(name string, level int) string {
	return fmt.Sprintf(`
resource "mssql_database" "test" {
  name                = %q
  compatibility_level = %d
}
`, name, level)
}
