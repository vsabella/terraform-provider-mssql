## 0.1.0 (Unreleased)

FEATURES:

* provider: add `enabled` provider argument for safe alias disable/teardown workflows
* login: add `sid` and `auto_import` support for safer adoption and drift-free login management
* user: support optional `login_name` for login-mapped users while preserving contained-user flows

ENHANCEMENTS:

* sqlclient: make role/user/grant/server-role deletions idempotent when target databases or objects are already missing
* sqlclient: harden Azure login handling with `DEFAULT_DATABASE` fallback behavior when unsupported
* provider: add resource guard utilities for provider readiness and strict server ID mismatch checks
* resources: use decoded resource IDs to validate provider/server alignment during read and delete paths

BUG FIXES:

* role: fix create error diagnostics to report role name consistently
* login: read/delete login metadata from `sys.server_principals` with `sys.sql_logins` fallback fields for broader compatibility
