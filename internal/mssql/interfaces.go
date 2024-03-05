package mssql

import "context"

type SqlClient interface {
	GetUser(ctx context.Context, username string) (User, error)
	CreateUser(ctx context.Context, create CreateUser) (User, error)
	UpdateUser(ctx context.Context, update UpdateUser) (User, error)
	DeleteUser(ctx context.Context, username string) error
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
	DefaultSchema string
}
