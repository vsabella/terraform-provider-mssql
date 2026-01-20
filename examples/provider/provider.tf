terraform {
  required_providers {
    mssql = {
      source = "registry.terraform.io/vsabella/mssql"
    }
  }
}

variable "mssql_password" {
  type      = string
  sensitive = true
}

provider "mssql" {
  host = "127.0.0.1"

  sql_auth {
    username = "sa"
    password = var.mssql_password
  }
}
