package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	_ "github.com/microsoft/go-mssqldb"
	"strings"
)

type client struct {
	connect func(ctx context.Context) (*sql.DB, error)
}

func NewClient(host string, port int64, database string, username string, password string) SqlClient {
	if port <= 0 {
		port = 1433
	}

	c := client{
		connect: func(ctx context.Context) (*sql.DB, error) {
			connString := fmt.Sprintf("server=%s;user id=%s;password=%s;port=%d;database=%s", host, username, password, port, database)
			conn, err := sql.Open("sqlserver", connString)
			return conn, err
		},
	}

	return c
}

func (m client) GetUser(ctx context.Context, username string) (User, error) {
	user := User{
		Id: username,
	}

	conn, err := m.connect(nil)
	if err != nil {
		return user, err
	}
	defer conn.Close()

	cmd := `SELECT 
    P.[name] AS id, 
    COALESCE(CONVERT(varchar(175), P.[sid],1), '') AS sid,
    P.[name] AS name, 
    P.[type] AS type,
    CASE WHEN P.[type] IN ('E', 'X') THEN 1 ELSE 0 END AS ext, 
    COALESCE(L.[name], '') AS login,
    COALESCE(P.[default_schema_name], '') AS default_schema_name
FROM sys.database_principals P
LEFT JOIN sys.sql_logins L ON P.sid = L.sid
WHERE P.[name] = @username`

	tflog.Debug(ctx, fmt.Sprintf("Executing refresh query for username %s: command %s", username, cmd))
	result := conn.QueryRowContext(ctx, cmd, sql.Named("username", username))

	err = result.Scan(&user.Id, &user.Sid, &user.Username, &user.Type, &user.External, &user.Login, &user.DefaultSchema)
	return user, err
}

func (m client) CreateUser(ctx context.Context, create CreateUser) (User, error) {
	var user User
	cmd, args, err := buildCreateUser(create)
	if err != nil {
		return user, err
	}

	conn, err := m.connect(nil)
	if err != nil {
		return user, err
	}
	defer conn.Close()

	_, err = conn.ExecContext(ctx,
		cmd,
		args...,
	)
	if err != nil {
		return user, err
	}

	user, err = m.GetUser(ctx, create.Username)
	return user, err
}

func buildCreateUser(create CreateUser) (string, []any, error) {
	var cmdBuilder strings.Builder
	var args []any

	if create.Login != "" && create.Password != "" {
		return "", nil, errors.New(fmt.Sprintf("invalid user %s, login users may not have passwords", create.Username))
	}

	if create.External && create.Password != "" {
		return "", nil, errors.New(fmt.Sprintf("invalid user %s, external users may not have passwords", create.Username))
	}

	if create.External && create.Login != "" {
		return "", nil, errors.New(fmt.Sprintf("invalid user %s, external users must not have a login", create.Username))
	}

	if create.External && create.Sid != "" {
		return "", nil, errors.New(fmt.Sprintf("invalid user %s, external users must not have a SID", create.Username))
	}

	if create.DefaultSchema == "" {
		return "", nil, errors.New(fmt.Sprintf("invalid user %s, default schema must be specified", create.Username))
	}

	cmdBuilder.WriteString("DECLARE @sql NVARCHAR(max);\n")
	cmdBuilder.WriteString("SET @sql = 'CREATE USER ' + QUOTENAME(@username)")
	args = append(args, sql.Named("username", create.Username))

	// Non Options
	if create.External {
		cmdBuilder.WriteString(" + ' FROM EXTERNAL PROVIDER'")
	}

	if create.Login != "" {
		cmdBuilder.WriteString(" + ' FROM LOGIN ' + QUOTENAME(@login)")
		args = append(args, sql.Named("login", create.Login))
	}

	// Begin Options. Easy since we make DefaultSchema required
	cmdBuilder.WriteString(" + ' WITH DEFAULT_SCHEMA = ' + QUOTENAME(@defaultSchema)")
	args = append(args, sql.Named("defaultSchema", create.DefaultSchema))

	if create.Password != "" {
		cmdBuilder.WriteString(" + ', PASSWORD = ' + QUOTENAME(@password, '''')")
		args = append(args, sql.Named("password", create.Password))

		if create.Sid != "" {
			cmdBuilder.WriteString(" + ', SID = ' + QUOTENAME(@sid, '''')")
			args = append(args, sql.Named("sid", create.Sid))
		}
	}

	cmdBuilder.WriteString(";\n")
	cmdBuilder.WriteString("EXEC (@sql);")
	return cmdBuilder.String(), args, nil
}

func addOption(builder *strings.Builder, args *[]any, name string, value string, identifier bool) {
	if value != "" {
		if builder.Len() == 0 {
			builder.WriteString(" + 'WITH '")
		} else {
			builder.WriteString(" + ', '")
		}

		if identifier {
			builder.WriteString(fmt.Sprintf(" + '%s = ' + QUOTENAME(@%s)", name, strings.ToLower(name)))
		} else {
			builder.WriteString(fmt.Sprintf(" + '%s = ' + QUOTENAME(@%s,'''')", name, strings.ToLower(name)))
		}

		*args = append(*args, sql.Named(strings.ToLower(name), value))
	}
}

func (m client) UpdateUser(ctx context.Context, update UpdateUser) (User, error) {
	var cmdBuilder strings.Builder
	var optionsBuilder strings.Builder
	var args []any

	addOption(&optionsBuilder, &args, "PASSWORD", update.Password, false)
	addOption(&optionsBuilder, &args, "DEFAULT_SCHEMA", update.DefaultSchema, true)

	if optionsBuilder.Len() > 0 {

		cmdBuilder.WriteString("DECLARE @sql NVARCHAR(max);\n")
		cmdBuilder.WriteString("SET @sql = 'ALTER USER ' + QUOTENAME(@username)")
		args = append(args, sql.Named("username", update.Id))

		cmdBuilder.WriteString(optionsBuilder.String())
		cmdBuilder.WriteString(";\n")
		cmdBuilder.WriteString("EXEC (@sql);")

		conn, err := m.connect(nil)
		if err != nil {
			return User{}, err
		}
		defer conn.Close()

		cmd := cmdBuilder.String()
		tflog.Debug(ctx, fmt.Sprintf("Updating User %s: cmd: %s", update.Id, cmd))

		_, err = conn.ExecContext(ctx,
			cmd,
			args...,
		)

		if err != nil {
			return User{}, err
		}
	}

	return m.GetUser(ctx, update.Id)

}

func (m client) DeleteUser(ctx context.Context, username string) error {
	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'IF EXISTS (SELECT 1 FROM [sys].[database_principals] WHERE [type] IN (''E'',''S'',''X'') AND [name] = ' + QUOTENAME(@p1, '''') + ') DROP USER ' + QUOTENAME(@p2);
          EXEC (@sql);`

	conn, err := m.connect(nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	tflog.Debug(ctx, fmt.Sprintf("Deleting User %s: cmd: %s", username, cmd))
	_, err = conn.ExecContext(ctx,
		cmd,
		username,
		username,
	)

	return err
}
