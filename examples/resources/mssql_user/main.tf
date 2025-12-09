terraform {
  required_providers {
    mssql = {
      source = "registry.terraform.io/vsabella/mssql"
    }
  }
}

provider "mssql" {
  host     = "127.0.0.1"
  database = "master"
  sql_auth = {
    username = "sa"
    password = "Testing@6CD21E2E-7028-4AE0-923E-B11288822489"
  }
}

# Managed database for all resources in this example
resource "mssql_database" "example_db" {
  name = "example_db"

  collation                     = "SQL_Latin1_General_CP1_CI_AS"
  read_committed_snapshot       = true
  allow_snapshot_isolation      = true
  accelerated_database_recovery = true

  scoped_configuration {
    name  = "LEGACY_CARDINALITY_ESTIMATION"
    value = "OFF"
  }

  scoped_configuration {
    name                = "MAXDOP"
    value               = "8"
    value_for_secondary = "4"
  }
}

# Login + user bound to the login (traditional model) in example_db
resource "mssql_login" "example" {
  name     = "example_login"
  password = "ExampleLoginP@ssw0rd!"
}

resource "mssql_user" "example_login_user" {
  database       = mssql_database.example_db.name
  username       = "example_login_user"
  login_name     = mssql_login.example.name
  default_schema = "dbo"
}

# Grant database-level SELECT to the login-backed user in example_db
resource "mssql_grant" "example_select" {
  database   = mssql_database.example_db.name
  permission = "SELECT"
  principal  = mssql_user.example_login_user.username
}

# Example role assignments for the login-backed user
resource "mssql_role_assignment" "example" {
  for_each = toset(["db_ddladmin", "db_datareader", "db_datawriter"])

  database  = mssql_database.example_db.name
  role      = each.key
  principal = mssql_user.example_login_user.username
}

###############################################################################
# Script resource example (creates a proc in example_db)
###############################################################################

resource "mssql_script" "example_script" {
  database_name = mssql_database.example_db.name
  name          = "example_script"

  create_script = <<-SQL
    IF OBJECT_ID('dbo.sp_example_status', 'P') IS NOT NULL
      DROP PROCEDURE dbo.sp_example_status;
    GO
    CREATE PROCEDURE dbo.sp_example_status
    AS
    BEGIN
      SET NOCOUNT ON;
      SELECT 1 AS status, DB_NAME() AS dbname;
    END;
  SQL

  delete_script = <<-SQL
    DROP PROCEDURE IF EXISTS dbo.sp_example_status;
  SQL

  version = "v1"
}

output "login_user" {
  value     = mssql_user.example_login_user
  sensitive = true
}
