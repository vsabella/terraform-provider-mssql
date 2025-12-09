package mssql

import (
	"context"
)

// SqlClient defines the interface for SQL Server operations.
// All database-scoped operations take a database parameter - pass empty string to use the provider's default database.
type SqlClient interface {
	// User operations (database-scoped)
	// database: target database (empty = provider's default)
	GetUser(ctx context.Context, database string, username string) (User, error)
	CreateUser(ctx context.Context, database string, create CreateUser) (User, error)
	UpdateUser(ctx context.Context, database string, update UpdateUser) (User, error)
	DeleteUser(ctx context.Context, database string, username string) error

	// Database role operations (database-scoped)
	// database: target database (empty = provider's default)
	GetRole(ctx context.Context, database string, name string) (Role, error)
	CreateRole(ctx context.Context, database string, name string) (Role, error)
	UpdateRole(ctx context.Context, database string, role Role) (Role, error)
	DeleteRole(ctx context.Context, database string, name string) error

	// Role membership operations
	// For database roles: use AssignRole/UnassignRole with database parameter
	// For server roles: use AssignServerRole/UnassignServerRole (no database needed)
	ReadRoleMembership(ctx context.Context, database string, role string, principal string) (RoleMembership, error)
	AssignRole(ctx context.Context, database string, role string, principal string) (RoleMembership, error)
	UnassignRole(ctx context.Context, database string, role string, principal string) error
	ReadServerRoleMembership(ctx context.Context, role string, principal string) (RoleMembership, error)
	AssignServerRole(ctx context.Context, role string, principal string) (RoleMembership, error)
	UnassignServerRole(ctx context.Context, role string, principal string) error

	// Permission operations
	// GrantPermission.Database specifies the target database (empty = provider's default)
	ReadPermission(ctx context.Context, grant GrantPermission) (GrantPermission, error)
	GrantPermission(ctx context.Context, grant GrantPermission) (GrantPermission, error)
	RevokePermission(ctx context.Context, grant GrantPermission) error

	// Database management operations (server-level, always work)
	GetDatabase(ctx context.Context, name string) (Database, error)
	CreateDatabase(ctx context.Context, name string, collation string) (Database, error)

	// Login operations (server-level principals, no database needed)
	GetLogin(ctx context.Context, name string) (Login, error)
	CreateLogin(ctx context.Context, create CreateLogin) (Login, error)
	UpdateLogin(ctx context.Context, update UpdateLogin) (Login, error)
	DeleteLogin(ctx context.Context, name string) error

	// Database options operations (target database specified by name parameter)
	GetDatabaseOptions(ctx context.Context, name string) (DatabaseOptions, error)
	SetDatabaseOptions(ctx context.Context, name string, opts DatabaseOptions) error
	GetDatabaseScopedConfigurations(ctx context.Context, name string) ([]DatabaseScopedConfiguration, error)
	SetDatabaseScopedConfiguration(ctx context.Context, name string, config DatabaseScopedConfiguration) error
	ClearDatabaseScopedConfiguration(ctx context.Context, name string, configName string) error

	// Script execution (database specified in parameter)
	ExecScript(ctx context.Context, database string, script string) error
}

type User struct {
	Id            string
	Username      string
	Type          string
	Sid           string
	External      bool
	DefaultSchema string
}

type RoleMembership struct {
	Id     string
	Role   string
	Member string
}

type CreateUser struct {
	Username      string
	Password      string
	Sid           string
	External      bool
	DefaultSchema string
	LoginName     string // Optional: if set, creates user FOR LOGIN instead of contained user
}

type UpdateUser struct {
	Id            string
	Password      string
	DefaultSchema string
}

// GrantPermission represents a permission grant with optional object targeting
type GrantPermission struct {
	Id         string
	Database   string // Target database (empty = provider's database)
	Principal  string
	Permission string
	ObjectType string // Optional: SCHEMA, TABLE, VIEW, PROCEDURE, etc.
	ObjectName string // Optional: name of the object
}

type Role struct {
	Id   string
	Name string
}

type Database struct {
	Id   int64
	Name string
}

// DatabaseOptions represents ALTER DATABASE options.
// Pointer fields indicate optional settings - nil means "don't change this setting".
type DatabaseOptions struct {
	Collation                   string
	CompatibilityLevel          *int
	RecoveryModel               *string
	ReadCommittedSnapshot       *bool
	AllowSnapshotIsolation      *bool
	AcceleratedDatabaseRecovery *bool
	AutoClose                   *bool
	AutoShrink                  *bool
	AutoCreateStats             *bool
	AutoUpdateStats             *bool
	AutoUpdateStatsAsync        *bool
}

// DatabaseScopedConfiguration represents ALTER DATABASE SCOPED CONFIGURATION settings
type DatabaseScopedConfiguration struct {
	Name              string
	Value             string
	ValueForSecondary string
}

// Login represents a SQL Server login (server-level principal)
type Login struct {
	Name            string
	DefaultDatabase string
	DefaultLanguage string
	IsDisabled      bool
}

// CreateLogin contains parameters for creating a new login
type CreateLogin struct {
	Name            string
	Password        string
	DefaultDatabase string
	DefaultLanguage string
}

// UpdateLogin contains parameters for updating an existing login
type UpdateLogin struct {
	Name            string
	Password        string
	DefaultDatabase string
	DefaultLanguage string
}
