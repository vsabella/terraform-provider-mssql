terraform {
  required_providers {
    mssql = {
      source = "registry.terraform.io/vsabella/mssql"
    }
  }
}

provider "mssql" {
  host = "127.0.0.1"

  sql_auth = {
    username = "sa"
    password = var.mssql_sa_password
  }
}

variable "mssql_sa_password" {
  description = "SA password for SQL Server (used for local testing / examples)."
  type        = string
  sensitive   = true
}
