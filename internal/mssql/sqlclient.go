package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	_ "github.com/microsoft/go-mssqldb"
	"net/url"
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
	var optionsBuilder strings.Builder

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
	addOption(&optionsBuilder, &args, "DEFAULT_SCHEMA", create.DefaultSchema, true)
	addOption(&optionsBuilder, &args, "PASSWORD", create.Password, false)
	addOption(&optionsBuilder, &args, "SID", create.Sid, false)

	cmdBuilder.WriteString(optionsBuilder.String())
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
	addOption(&optionsBuilder, &args, "LOGIN", update.Login, true)

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

func encodeRoleMembershipId(role string, member string) string {
	return fmt.Sprintf("%s/%s", url.QueryEscape(role), url.QueryEscape(member))
}

func decodeRoleMembershipId(id string) (string, string, error) {
	re, me, found := strings.Cut(id, "/")
	if !found {
		return "", "", sql.ErrNoRows
	}

	role, err := url.QueryUnescape(re)
	if err != nil {
		return "", "", err
	}

	member, err := url.QueryUnescape(me)
	if err != nil {
		return "", "", err
	}

	return role, member, err
}

func (m client) ReadRoleMembership(ctx context.Context, id string) (RoleMembership, error) {
	var roleMembership RoleMembership

	role, member, err := decodeRoleMembershipId(id)
	if err != nil {
		return roleMembership, err
	}

	cmd := `SELECT r.name role_principal_name,
m.name AS member_principal_name
FROM sys.database_role_members rm
JOIN sys.database_principals r
ON rm.role_principal_id = r.principal_id
JOIN sys.database_principals m
ON rm.member_principal_id = m.principal_id
WHERE r.type = 'R'
AND R.name = @p1
AND M.name = @p2
`
	conn, err := m.connect(nil)
	if err != nil {
		return roleMembership, err
	}
	defer conn.Close()

	tflog.Debug(ctx, fmt.Sprintf("Reading Role Assignment role %s, member %s: cmd: %s", role, member, cmd))
	result := conn.QueryRowContext(ctx,
		cmd,
		role,
		member,
	)

	err = result.Scan(&roleMembership.Role, &roleMembership.Member)
	if err != nil {
		tflog.Warn(ctx, err.Error())
		return roleMembership, err
	}

	roleMembership.Id = encodeRoleMembershipId(roleMembership.Role, roleMembership.Member)

	tflog.Debug(ctx, fmt.Sprintf("SUCCESS Reading Role Assignment role %s, member %s: cmd: %s", role, member, cmd))
	return roleMembership, err
}

func (m client) AssignRole(ctx context.Context, role string, member string) (RoleMembership, error) {
	var roleMembership RoleMembership

	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'ALTER ROLE ' + QUOTENAME(@p1) + ' ADD MEMBER ' + QUOTENAME(@p2);
          EXEC (@sql);`

	conn, err := m.connect(nil)
	if err != nil {
		return roleMembership, err
	}
	defer conn.Close()

	tflog.Debug(ctx, fmt.Sprintf("Adding Principal %s to role %s: cmd: %s", member, role, cmd))
	_, err = conn.ExecContext(ctx,
		cmd,
		role,
		member,
	)

	if err != nil {
		return roleMembership, err
	}
	return m.ReadRoleMembership(ctx, encodeRoleMembershipId(role, member))
}

func (m client) UnassignRole(ctx context.Context, role string, principal string) error {
	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'ALTER ROLE ' + QUOTENAME(@p1) + ' DROP MEMBER ' + QUOTENAME(@p2);
          EXEC (@sql);`

	conn, err := m.connect(nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	tflog.Debug(ctx, fmt.Sprintf("Removing Principal %s from role %s: cmd: %s", principal, role, cmd))
	_, err = conn.ExecContext(ctx,
		cmd,
		role,
		principal,
	)

	return err
}

func (m client) ReadDatabasePermission(ctx context.Context, id string) (DatabasePermission, error) {
	var DatabasePermission DatabasePermission
	principal := strings.Split(id, "/")[0]
	permission := strings.Split(id, "/")[1]

	conn, err := m.connect(nil)
	if err != nil {
		return DatabasePermission, fmt.Errorf("failed to connect to the database: %v", err)
	}
	defer conn.Close()

	cmd := `
		SELECT
			dp.[name] AS [principal],
			sdp.[permission_name] AS [permission]
		FROM
			sys.database_permissions AS sdp
		JOIN
			sys.database_principals AS dp ON sdp.grantee_principal_id = dp.principal_id
		WHERE
			sdp.[class] = 0
			AND sdp.[state] IN ('G', 'W')
			AND dp.[name] = @p1
			AND sdp.[permission_name] = @p2;
	`

	tflog.Debug(ctx, fmt.Sprintf("Reading DB permission [principal: %s, permission: %s]", principal, permission))
	result := conn.QueryRowContext(ctx, cmd, principal, permission)

	err = result.Scan(&DatabasePermission.Principal, &DatabasePermission.Permission)
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("failed to scan result: %v", err))
		return DatabasePermission, err
	}

	DatabasePermission.Id = fmt.Sprintf("%s/%s", DatabasePermission.Principal, strings.ToLower(DatabasePermission.Permission))
	tflog.Debug(ctx, fmt.Sprintf("SUCCESS Reading DB permission principal: %s, permission: %s", principal, permission))

	return DatabasePermission, nil
}

func (m client) GrantDatabasePermission(ctx context.Context, principal string, permission string) (DatabasePermission, error) {
	var DatabasePermission DatabasePermission

	conn, err := m.connect(nil)
	if err != nil {
		return DatabasePermission, err
	}
	defer conn.Close()

	query := fmt.Sprintf("GRANT %s TO %s", permission, principal)

	tflog.Debug(ctx, fmt.Sprintf("Granting permission %s to %s [%s]", permission, principal, query))

	_, err = conn.ExecContext(ctx, query)
	if err != nil {
		return DatabasePermission, fmt.Errorf("failed to execute grant query: %v", err)
	}

	return m.ReadDatabasePermission(ctx, fmt.Sprintf("%s/%s", principal, strings.ToUpper(permission)))
}

func (m client) RevokeDatabasePermission(ctx context.Context, principal string, permission string) error {
	conn, err := m.connect(nil)
	if err != nil {
		return fmt.Errorf("failed to connect to the database: %v", err)
	}
	defer conn.Close()

	query := fmt.Sprintf("REVOKE %s TO %s CASCADE", permission, principal)

	tflog.Debug(ctx, fmt.Sprintf("Revoking permission %s from user %s", permission, principal))

	_, err = conn.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to execute revoke query: %v", err)
	}

	return nil
}
