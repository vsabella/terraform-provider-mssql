---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "mssql_user Resource - mssql"
subcategory: ""
description: |-
  MssqlUser resource
---

# mssql_user (Resource)

MssqlUser resource



<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `password` (String, Sensitive) Password for the login. Must follow strong password policies defined for SQL server. Passwords are case-sensitive, length must be 8-128 chars, can include all characters except `'` or `name`.

~> **Note** Password will be stored in the raw state as plain-text. [Read more about sensitive data in state](https://www.terraform.io/language/state/sensitive-data).
- `username` (String) MssqlUser configurable attribute with default value

### Optional

- `default_schema` (String)
- `external` (Boolean) Is this an external user (like Microsoft EntraID)
- `login` (String) Login to associate to this user
- `sid` (String) Set custom SID for the user

### Read-Only

- `id` (String) The ID of this resource.