// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/microsoft/go-mssqldb"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

const (
	// providerConfig is a shared configuration to combine with the actual
	// test configuration so the HashiCups client is properly configured.
	// It is also possible to use the HASHICUPS_ environment variables instead,
	// such as updating the Makefile and running the testing through that tool.
	providerConfig = `
provider "mssql" {
  host     = "127.0.0.1"
  database = "testdb"
  sql_auth = {
    username = "sa"
    password = "Testing@6CD21E2E-7028-4AE0-923E-B11288822489"
  }
}
`
)

// testAccProtoV6ProviderFactories are used to instantiate a provider during
// acceptance testing. The factory function will be invoked for every Terraform
// CLI command executed to create a provider server to which the CLI can
// reattach.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"mssql": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	// You can add code here to run prior to any test case execution, for example assertions
	// about the appropriate environment variables being set are common to see in a pre-check
	// function.
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set; skipping acceptance tests")
	}

	pw := os.Getenv("MSSQL_SA_PASSWORD")
	if pw == "" {
		t.Fatal("MSSQL_SA_PASSWORD must be set for acceptance tests")
	}

	if err := cleanupTestDatabases(pw); err != nil {
		t.Fatalf("failed to cleanup test databases: %v", err)
	}
}

// cleanupTestDatabases drops known test databases to avoid collision across acceptance tests.
func cleanupTestDatabases(saPassword string) error {
	// Keep this list in sync with acceptance test configs
	dbNames := []string{
		"test_database",
		"test_db_options",
		"test_db_compat",
		"test_db_collation",
		"test_db_scoped",
		"test_db_one",
		"test_db_two",
		"testdb_schema_obj",
		"test_role_db",
		"test_role_assign_db",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	connStr := fmt.Sprintf("sqlserver://sa:%s@127.0.0.1:1433?database=master&encrypt=disable", url.QueryEscape(saPassword))
	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return err
	}
	defer db.Close()

	for _, name := range dbNames {
		stmt := fmt.Sprintf("IF DB_ID('%s') IS NOT NULL BEGIN ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE [%s]; END", name, name, name)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("drop %s: %w", name, err)
		}
	}
	return nil
}
