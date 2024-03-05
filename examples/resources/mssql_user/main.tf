terraform {
  required_providers {
    mssql = {
      source = "registry.terraform.io/openaxon/mssql"
    }
  }
}

provider "mssql" {
  host     = "127.0.0.1"
  database = "example"
  sql_auth = {
    username = "sa"
    password = "Ax@0n9A9REQF4TCgdKP0KrZC"
  }
}

resource "mssql_user" "example" {
  username = "exampleuser"
  password = "AXzN@123451#@#293923293@@#@#!!@#"
}

resource "mssql_role_assignment" "example" {
  for_each = toset(["db_ddladmin", "db_datareader", "db_datawriter"])

  role      = each.key
  principal = mssql_user.example.username
}

output "user" {
  value     = mssql_user.example
  sensitive = true
}
