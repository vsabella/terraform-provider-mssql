terraform {
  required_providers {
    mssql = {
      source = "registry.terraform.io/openaxon/mssql"
    }
  }
}

provider "mssql" {
  host = "127.0.0.1"
  sql_auth = {
    username = "sa"
    password = "Ax@0n9A9REQF4TCgdKP0KrZC"
  }
}

resource "mssql_user" "example" {
  database = "example"
  username = "example-user"
  password = "1234"
}

resource "mssql_user" "example2" {
  database = "example"
  username = "example-user"
  password = "1234"
}

output "user" {
  value = mssql_user.example
  sensitive = true
}
