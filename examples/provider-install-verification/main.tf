terraform {
  required_providers {
    mssql = {
      source = "registry.terraform.io/openaxon/mssql"
    }
  }
}

provider "mssql" {}

data "mssql_user" "example" {}
