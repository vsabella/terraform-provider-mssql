# Terraform Provider for Microsoft SQL Server

This provider allows you to manage Microsoft SQL Server resources using Terraform. It supports both SQL Server authentication and Azure AD authentication.

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.19
- Microsoft SQL Server instance (on-premises or Azure SQL Database)

## Building The Provider

1. Clone the repository
```bash
git clone https://github.com/vsabella/terraform-provider-mssql.git
cd terraform-provider-mssql
```

2. Build the provider
```bash
go build -o terraform-provider-mssql
```

3. Install the provider
```bash
mkdir -p ~/.terraform.d/plugins/registry.terraform.io/vsabella/mssql/0.1.0/darwin_amd64
cp terraform-provider-mssql ~/.terraform.d/plugins/registry.terraform.io/vsabella/mssql/0.1.0/darwin_amd64/
```

## Using the provider

### Provider Configuration

The provider supports two authentication methods:

1. SQL Authentication:
```hcl
provider "mssql" {
  host     = "your-server.database.windows.net"
  port     = 1433
  database = "your-database"
  sql_auth = {
    username = "your-username"
    password = "your-password"
  }
}
```

2. Azure AD Authentication:
```hcl
provider "mssql" {
  host         = "your-server.database.windows.net"
  port         = 1433
  database     = "your-database"
  azure_ad_auth = true
}
```

### Available Resources

#### MSSQL User
Creates a database user in SQL Server.

Using a username and password:

```hcl
resource "mssql_user" "example" {
  username        = "example_user"
  password        = "example_password"
  default_schema  = "dbo"
  external        = false
}
```

Using a Managed Identity:

```hcl
resource "mssql_user" "example" {
  username        = "managed_identity_name"
  default_schema  = "dbo"
  external        = true
}
```

#### MSSQL Role
Creates a database role.

```hcl
resource "mssql_role" "example" {
  name = "example_role"
}
```

#### MSSQL Role Assignment
Assigns a user to a role.

```hcl
resource "mssql_role_assignment" "example" {
  role   = mssql_role.example.name
  member = mssql_user.example.username
}
```

#### MSSQL Grant
Grants database permissions to a user.

```hcl
resource "mssql_grant" "example" {
  principal  = mssql_user.example.username
  permission = "SELECT"
}
```

### Example Usage

Here's a complete example showing how to create a user, role, and assign permissions:

```hcl
terraform {
  required_providers {
    mssql = {
      source = "vsabella/mssql"
    }
  }
}

provider "mssql" {
  host     = "your-server.database.windows.net"
  port     = 1433
  database = "your-database"
  sql_auth = {
    username = "your-username"
    password = "your-password"
  }
}

# Create a database user
resource "mssql_user" "app_user" {
  username       = "app_user"
  password       = "secure_password"
  default_schema = "dbo"
  external       = false
}

# Create a role for the application
resource "mssql_role" "app_role" {
  name = "app_role"
}

# Assign the user to the role
resource "mssql_role_assignment" "app_role_assignment" {
  role   = mssql_role.app_role.name
  member = mssql_user.app_user.username
}

# Grant necessary permissions
resource "mssql_grant" "app_permissions" {
  principal  = mssql_user.app_user.username
  permission = "SELECT"
}
```

## Development

### Running Tests

```bash
go test ./...
```

### Running Acceptance Tests

```bash
TF_ACC=1 go test ./...
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MPL-2.0 License - see the [LICENSE](LICENSE) file for details.
