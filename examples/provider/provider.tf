terraform {
  required_providers {
    mssql = {
      source = "registry.terraform.io/vsabella/mssql"
    }
  }
}

provider "mssql" {}
