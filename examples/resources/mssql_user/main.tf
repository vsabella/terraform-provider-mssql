terraform {
  required_providers {
    mssql = {
      source = "registry.terraform.io/openaxon/mssql"
    }
  }
}

provider "mssql" {
  host = "127.0.0.1"
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

output "user" {
  value = mssql_user.example
  sensitive = true
}
