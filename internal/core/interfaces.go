package core

import "context"

type SqlClient interface {
	GetUser(username string) (User, error)
	CreateUser(user User) (User, error)
	UpdateUser(user User) (User, error)
	DeleteUser(username string) error
}

type ProviderData struct {
	ClientFactory func(ctx context.Context, db string) (SqlClient, error)
}
