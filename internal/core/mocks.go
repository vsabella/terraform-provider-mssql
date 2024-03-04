package core

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
)

type MockSqlClient struct {
	users map[string]User
}

func (client *MockSqlClient) GetUser(username string) (User, error) {
	fmt.Printf("GetUser %s", username)

	return client.users[username], nil
}

func (client *MockSqlClient) CreateUser(user User) (User, error) {
	fmt.Printf("CreateUser %s", user.Username)

	if _, ok := client.users[user.Username]; ok {
		return user, errors.New("user already exists")
	} else {
		user.Id = uuid.NewString()
		client.users[user.Username] = user
		return user, nil
	}
}

func (client *MockSqlClient) UpdateUser(user User) (User, error) {
	fmt.Printf("UpdateUser %s", user.Username)

	if _, ok := client.users[user.Username]; ok {
		client.users[user.Username] = user
		return user, nil

	} else {
		return user, errors.New("user does not exist")
	}
}

func (client *MockSqlClient) DeleteUser(username string) error {
	fmt.Printf("DeleteUser %s", username)

	if _, ok := client.users[username]; ok {
		delete(client.users, username)
		return nil

	} else {
		return errors.New("user does not exist")
	}
}

func NewMockSqlClient() *SqlClient {
	var mockClient SqlClient = &MockSqlClient{
		users: map[string]User{},
	}

	return &mockClient
}

type MockClientFactory struct {
	clients map[string]*SqlClient
}

func (factory *MockClientFactory) GetClient(db string) SqlClient {
	if client, ok := factory.clients[db]; ok {
		return *client
	} else {
		client := NewMockSqlClient()
		factory.clients[db] = client
		return *client
	}
}

func NewMockClientFactory() *MockClientFactory {
	factory := MockClientFactory{
		clients: map[string]*SqlClient{},
	}

	return &factory
}
