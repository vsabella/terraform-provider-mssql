# Terraform Provider for Microsoft SQL Server

This provider allows you to manage Microsoft SQL Server resources using Terraform. It provides a way to interact with SQL Server databases, tables, and other database objects as infrastructure as code.

## Features

- Manage SQL Server databases and their configurations
- Create and manage database tables
- Configure database users and permissions
- Support for SQL Server authentication

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.21
- Microsoft SQL Server instance (local or remote)

## Installation

### Using Terraform Registry

Add the following to your Terraform configuration:

```hcl
terraform {
  required_providers {
    mssql = {
      source = "vsabella/mssql"
      version = "~> 0.1.0"  # Replace with the latest version
    }
  }
}
```

### Building from Source

1. Clone the repository:
```shell
git clone https://github.com/vsabella/terraform-provider-mssql.git
cd terraform-provider-mssql
```

2. Build the provider:
```shell
go install
```

## Provider Configuration

Configure the provider in your Terraform configuration:

```hcl
provider "mssql" {
  host     = "your-sql-server-host"
  port     = 1433
  database = "your-database-name"
  sql_auth {
    username = "your-username"
    password = "your-password"
  }
}
```

### Provider Arguments

- `host` - (Required) The hostname or IP address of the SQL Server instance
- `port` - (Optional) The port number for the SQL Server instance (default: 1433)
- `database` - (Required) The name of the database to connect to
- `sql_auth` - (Optional) SQL Server authentication block. When provided, SQL authentication will be used when connecting.
  - `username` - (Required when using SQL authentication) The SQL Server username
  - `password` - (Required when using SQL authentication) The SQL Server password

## Resources

### Database

```hcl
resource "mssql_database" "example" {
  name = "example_database"
  collation = "SQL_Latin1_General_CP1_CI_AS"
}
```

### Table

```hcl
resource "mssql_table" "example" {
  database_id = mssql_database.example.id
  name = "example_table"
  schema = "dbo"
  columns = [
    {
      name = "id"
      type = "int"
      nullable = false
    },
    {
      name = "name"
      type = "varchar"
      length = 255
      nullable = true
    }
  ]
}
```

## Data Sources

### Database

```hcl
data "mssql_database" "example" {
  name = "example_database"
}
```

## Development

### Building the Provider

To compile the provider, run:
```shell
go install
```

### Running Tests

To run the full suite of acceptance tests:
```shell
make testacc
```

Note: Acceptance tests require Docker and docker-compose. They create an empty local SQL Server container to run the tests. See [ci/run_acceptance.sh](ci/run_acceptance.sh) for more details.

### Generating Documentation

To generate or update documentation:
```shell
go generate
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MPL-2.0 License - see the [LICENSE](LICENSE) file for details.
