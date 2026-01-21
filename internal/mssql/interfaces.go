package mssql

import (
	"context"
)

type SqlClient interface {
	// Database-scoped operations
	GetUser(ctx context.Context, database string, username string) (User, error)
	CreateUser(ctx context.Context, database string, create CreateUser) (User, error)
	UpdateUser(ctx context.Context, database string, update UpdateUser) (User, error)
	DeleteUser(ctx context.Context, database string, username string) error

	ReadRoleMembership(ctx context.Context, database string, id string) (RoleMembership, error)
	AssignRole(ctx context.Context, database string, role string, principal string) (RoleMembership, error)
	UnassignRole(ctx context.Context, database string, role string, principal string) error

	ReadDatabasePermission(ctx context.Context, database string, id string) (DatabaseGrantPermission, error)
	GrantDatabasePermission(ctx context.Context, database string, principal string, permission string) (DatabaseGrantPermission, error)
	RevokeDatabasePermission(ctx context.Context, database string, principal string, permission string) error

	GetRole(ctx context.Context, database string, name string) (Role, error)
	CreateRole(ctx context.Context, database string, name string) (Role, error)
	UpdateRole(ctx context.Context, database string, role Role) (Role, error)
	DeleteRole(ctx context.Context, database string, name string) error

	// Server-scoped operations
	GetDatabase(ctx context.Context, name string) (Database, error)
	GetDatabaseById(ctx context.Context, id int64) (Database, error)
	CreateDatabase(ctx context.Context, name string) (Database, error)

	// ExecScript executes an arbitrary SQL script in the specified database.
	// If database is empty, the provider's configured database is used.
	ExecScript(ctx context.Context, database string, script string) error
}

type User struct {
	Id            string
	Username      string
	Type          string
	Sid           string
	External      bool
	DefaultSchema string
	// LoginName is set for login-mapped users (CREATE USER ... FOR LOGIN ...).
	// It will be empty for contained database users and external users.
	LoginName string
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
}

type UpdateUser struct {
	Id            string
	Password      string
	DefaultSchema string
}

type DatabaseGrantPermission struct {
	Id         string
	Principal  string
	Permission string
}

type Role struct {
	Id   string
	Name string
}

type Database struct {
	Id   int64
	Name string
}
