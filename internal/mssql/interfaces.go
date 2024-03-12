package mssql

import "context"

type SqlClient interface {
	GetUser(ctx context.Context, username string) (User, error)
	CreateUser(ctx context.Context, create CreateUser) (User, error)
	UpdateUser(ctx context.Context, update UpdateUser) (User, error)
	DeleteUser(ctx context.Context, username string) error
	ReadRoleMembership(ctx context.Context, id string) (RoleMembership, error)
	AssignRole(ctx context.Context, role string, principal string) (RoleMembership, error)
	UnassignRole(ctx context.Context, role string, principal string) error
	ReadDatabasePermission(ctx context.Context, id string) (DatabasePermission, error)
	GrantDatabasePermission(ctx context.Context, principal string, permission string) (DatabasePermission, error)
	RevokeDatabasePermission(ctx context.Context, principal string, permission string) error
}

type User struct {
	Id            string
	Username      string
	Type          string
	Sid           string
	External      bool
	Login         string
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
	Login         string
	DefaultSchema string
}

type UpdateUser struct {
	Id            string
	Password      string
	Login         string
	DefaultSchema string
}

type DatabasePermission struct {
	Id          string
	Principal	string
	Permission	string
}
