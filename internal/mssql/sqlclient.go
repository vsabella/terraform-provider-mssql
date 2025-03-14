package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	_ "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/azuread"
)

type client struct {
	conn *sql.DB
}

func NewClient(host string, port int64, database string, username string, password string) SqlClient {
	if port <= 0 {
		port = 1433
	}

	connString := fmt.Sprintf("server=%s;user id=%s;password=%s;port=%d;database=%s", host, username, password, port, database)
	conn, err := sql.Open("sqlserver", connString)

	if err != nil {
		// TODO handle error
		panic(err)
	}

	c := client{
		conn: conn,
	}
	return c
}

// NewAzureADClient creates a new SQL client authenticated via Azure AD.
func NewAzureADClient(host string, port int64, database string) (SqlClient, error) {
	if port <= 0 {
		port = 1433
	}

	connString := fmt.Sprintf("server=%s;database=%s;port=%d;fedauth=ActiveDirectoryDefault;", host, database, port)
	conn, err := sql.Open(azuread.DriverName, connString)

	if err != nil {
		return nil, fmt.Errorf("failed to connect to SQL Server. Error: %v", err)
	} else if conn == nil {
		return nil, fmt.Errorf("failed to connect to SQL Server. conn == nil")
	}

	// Test the connection
	err = conn.Ping()
	if err != nil {
		return nil, fmt.Errorf("failed to ping SQL Server: %v", err)
	}

	return &client{conn: conn}, nil
}

func (m client) GetUser(ctx context.Context, username string) (User, error) {
	user := User{
		Id: username,
	}

	cmd := `SELECT
    P.[name] AS id,
    COALESCE(CONVERT(varchar(175), P.[sid],1), '') AS sid,
    P.[name] AS name,
    P.[type] AS type,
    CASE WHEN P.[type] IN ('E', 'X') THEN 1 ELSE 0 END AS ext,
    COALESCE(P.[default_schema_name], '') AS default_schema_name
FROM sys.database_principals P
WHERE P.[name] = @username`

	tflog.Debug(ctx, fmt.Sprintf("Executing refresh query for username %s: command %s", username, cmd))
	result := m.conn.QueryRowContext(ctx, cmd, sql.Named("username", username))

	err := result.Scan(&user.Id, &user.Sid, &user.Username, &user.Type, &user.External, &user.DefaultSchema)
	return user, err
}

func (m client) CreateUser(ctx context.Context, create CreateUser) (User, error) {
	var user User
	cmd, args, err := buildCreateUser(create)
	if err != nil {
		return user, err
	}

	_, err = m.conn.ExecContext(ctx,
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

	if create.External && create.Password != "" {
		return "", nil, fmt.Errorf("invalid user %s, external users may not have passwords", create.Username)
	}

	if create.External && create.Sid != "" {
		return "", nil, fmt.Errorf("invalid user %s, external users must not have a SID", create.Username)
	}

	if create.DefaultSchema == "" {
		return "", nil, fmt.Errorf("invalid user %s, default schema must be specified", create.Username)
	}

	cmdBuilder.WriteString("DECLARE @sql NVARCHAR(max);\n")
	cmdBuilder.WriteString("SET @sql = 'CREATE USER ' + QUOTENAME(@username)")
	args = append(args, sql.Named("username", create.Username))

	// Non Options
	if create.External {
		cmdBuilder.WriteString(" + ' FROM EXTERNAL PROVIDER '")
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

	if optionsBuilder.Len() > 0 {

		cmdBuilder.WriteString("DECLARE @sql NVARCHAR(max);\n")
		cmdBuilder.WriteString("SET @sql = 'ALTER USER ' + QUOTENAME(@username)")
		args = append(args, sql.Named("username", update.Id))

		cmdBuilder.WriteString(optionsBuilder.String())
		cmdBuilder.WriteString(";\n")
		cmdBuilder.WriteString("EXEC (@sql);")

		cmd := cmdBuilder.String()
		tflog.Debug(ctx, fmt.Sprintf("Updating User %s: cmd: %s", update.Id, cmd))

		_, err := m.conn.ExecContext(ctx,
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

	tflog.Debug(ctx, fmt.Sprintf("Deleting User %s: cmd: %s", username, cmd))
	_, err := m.conn.ExecContext(ctx,
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

	tflog.Debug(ctx, fmt.Sprintf("Reading Role Assignment role %s, member %s: cmd: %s", role, member, cmd))

	result := m.conn.QueryRowContext(ctx,
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

	tflog.Debug(ctx, fmt.Sprintf("Adding Principal %s to role %s: cmd: %s", member, role, cmd))
	_, err := m.conn.ExecContext(ctx,
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

	tflog.Debug(ctx, fmt.Sprintf("Removing Principal %s from role %s: cmd: %s", principal, role, cmd))
	_, err := m.conn.ExecContext(ctx,
		cmd,
		role,
		principal,
	)

	return err
}

func (m client) ReadDatabasePermission(ctx context.Context, id string) (DatabaseGrantPermission, error) {
	var DatabaseGrantPermission DatabaseGrantPermission
	principal := strings.Split(id, "/")[0]
	permission := strings.Split(id, "/")[1]

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
	result := m.conn.QueryRowContext(ctx, cmd, principal, permission)

	err := result.Scan(&DatabaseGrantPermission.Principal, &DatabaseGrantPermission.Permission)
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("failed to scan result: %v", err))
		return DatabaseGrantPermission, err
	}

	DatabaseGrantPermission.Id = fmt.Sprintf("%s/%s", DatabaseGrantPermission.Principal, strings.ToLower(DatabaseGrantPermission.Permission))
	tflog.Debug(ctx, fmt.Sprintf("SUCCESS Reading DB permission principal: %s, permission: %s", principal, permission))

	return DatabaseGrantPermission, nil
}

func (m client) GrantDatabasePermission(ctx context.Context, principal string, permission string) (DatabaseGrantPermission, error) {
	var DatabaseGrantPermission DatabaseGrantPermission

	query := fmt.Sprintf("GRANT %s TO %s", permission, principal)

	tflog.Debug(ctx, fmt.Sprintf("Granting permission %s to %s [%s]", permission, principal, query))

	_, err := m.conn.ExecContext(ctx, query)
	if err != nil {
		return DatabaseGrantPermission, fmt.Errorf("failed to execute grant query: %v", err)
	}

	return m.ReadDatabasePermission(ctx, fmt.Sprintf("%s/%s", principal, strings.ToUpper(permission)))
}

func (m client) RevokeDatabasePermission(ctx context.Context, principal string, permission string) error {
	query := fmt.Sprintf("REVOKE %s TO %s CASCADE", permission, principal)

	tflog.Debug(ctx, fmt.Sprintf("Revoking permission %s from user %s", permission, principal))

	_, err := m.conn.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to execute revoke query: %v", err)
	}

	return nil
}

func (m client) GetRole(ctx context.Context, name string) (Role, error) {
	role := Role{
		Id:   name,
		Name: name,
	}

	cmd := `SELECT [name] FROM sys.database_principals WHERE [type] = 'R' AND [name] = @name`
	tflog.Debug(ctx, fmt.Sprintf("Executing refresh query for role %s: command %s", name, cmd))
	result := m.conn.QueryRowContext(ctx, cmd, sql.Named("name", name))
	err := result.Scan(&role.Id)
	return role, err
}

func (m client) CreateRole(ctx context.Context, name string) (Role, error) {
	var role Role
	query := fmt.Sprintf("CREATE ROLE [%s]", name)
	_, _ = m.conn.ExecContext(ctx, query)

	role, err := m.GetRole(ctx, name)
	return role, err
}

func (m client) UpdateRole(ctx context.Context, role Role) (Role, error) {
	var update Role
	// TODO update role.name to update
	update.Id = role.Id
	return m.GetRole(ctx, update.Id)
}

func (m client) DeleteRole(ctx context.Context, name string) error {
	query := fmt.Sprintf("DROP ROLE %s", name)
	tflog.Debug(ctx, fmt.Sprintf("Deleting Role %s: cmd: %s", name, query))
	_, err := m.conn.ExecContext(ctx, query)

	return err
}
