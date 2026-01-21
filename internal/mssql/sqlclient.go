package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	_ "github.com/microsoft/go-mssqldb"
)

type client struct {
	conn *sql.DB

	host     string
	port     int64
	database string
	username string
	password string

	connMu         sync.Mutex
	connByDatabase map[string]*sql.DB
}

func buildConnString(host string, port int64, database string, username string, password string) string {
	return fmt.Sprintf("server=%s;user id=%s;password=%s;port=%d;database=%s", host, username, password, port, database)
}

func NewClient(host string, port int64, database string, username string, password string) SqlClient {
	if port <= 0 {
		port = 1433
	}

	conn, err := sql.Open("sqlserver", buildConnString(host, port, database, username, password))

	if err != nil {
		// TODO handle error
		panic(err)
	}

	c := &client{
		conn:           conn,
		host:           host,
		port:           port,
		database:       database,
		username:       username,
		password:       password,
		connByDatabase: map[string]*sql.DB{},
	}

	// Seed the pool cache with the default database connection.
	if database != "" {
		c.connByDatabase[database] = conn
	}

	return c
}

// getConnForDatabase returns a pooled connection for a database.
//
// This is intentionally implemented as a cache of `*sql.DB` pools per database,
// so callers don't need to manage close semantics for per-database connections.
func (m *client) getConnForDatabase(database string) (*sql.DB, error) {
	if database == "" || database == m.database {
		return m.conn, nil
	}

	m.connMu.Lock()
	existing := m.connByDatabase[database]
	m.connMu.Unlock()
	if existing != nil {
		return existing, nil
	}

	// Create outside the lock to avoid blocking concurrent callers.
	newConn, err := sql.Open("sqlserver", buildConnString(m.host, m.port, database, m.username, m.password))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database %s: %v", database, err)
	}

	if err := newConn.Ping(); err != nil {
		newConn.Close()
		return nil, fmt.Errorf("failed to ping database %s: %v", database, err)
	}

	m.connMu.Lock()
	// Double-check to avoid duplicating pools in races.
	if existing = m.connByDatabase[database]; existing == nil {
		m.connByDatabase[database] = newConn
		m.connMu.Unlock()
		return newConn, nil
	}
	m.connMu.Unlock()

	newConn.Close()
	return existing, nil
}

func (m *client) GetUser(ctx context.Context, database string, username string) (User, error) {
	user := User{
		Id: username,
	}

	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return user, err
	}

	cmd := `SELECT
    P.[name] AS id,
    COALESCE(CONVERT(varchar(175), P.[sid],1), '') AS sid,
    P.[name] AS name,
    P.[type] AS type,
    CASE WHEN P.[type] IN ('E', 'X') THEN 1 ELSE 0 END AS ext,
    COALESCE(P.[default_schema_name], '') AS default_schema_name,
    COALESCE(SP.[name], '') AS login_name
FROM sys.database_principals P
LEFT JOIN sys.server_principals SP ON P.[sid] = SP.[sid]
WHERE P.[name] = @username`

	tflog.Debug(ctx, fmt.Sprintf("Executing refresh query for username %s: command %s", username, cmd))
	result := conn.QueryRowContext(ctx, cmd, sql.Named("username", username))

	err = result.Scan(&user.Id, &user.Sid, &user.Username, &user.Type, &user.External, &user.DefaultSchema, &user.LoginName)
	return user, err
}

func (m *client) CreateUser(ctx context.Context, database string, create CreateUser) (User, error) {
	var user User
	cmd, args, err := buildCreateUser(create)
	if err != nil {
		return user, err
	}

	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return user, err
	}

	_, err = conn.ExecContext(ctx,
		cmd,
		args...,
	)
	if err != nil {
		return user, err
	}

	user, err = m.GetUser(ctx, database, create.Username)
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

func (m *client) UpdateUser(ctx context.Context, database string, update UpdateUser) (User, error) {
	var cmdBuilder strings.Builder
	var optionsBuilder strings.Builder
	var args []any

	addOption(&optionsBuilder, &args, "PASSWORD", update.Password, false)
	addOption(&optionsBuilder, &args, "DEFAULT_SCHEMA", update.DefaultSchema, true)

	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return User{}, err
	}

	if optionsBuilder.Len() > 0 {

		cmdBuilder.WriteString("DECLARE @sql NVARCHAR(max);\n")
		cmdBuilder.WriteString("SET @sql = 'ALTER USER ' + QUOTENAME(@username)")
		args = append(args, sql.Named("username", update.Id))

		cmdBuilder.WriteString(optionsBuilder.String())
		cmdBuilder.WriteString(";\n")
		cmdBuilder.WriteString("EXEC (@sql);")

		cmd := cmdBuilder.String()
		tflog.Debug(ctx, fmt.Sprintf("Updating User %s: cmd: %s", update.Id, cmd))

		_, err := conn.ExecContext(ctx,
			cmd,
			args...,
		)

		if err != nil {
			return User{}, err
		}
	}

	return m.GetUser(ctx, database, update.Id)

}

func (m *client) DeleteUser(ctx context.Context, database string, username string) error {
	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'IF EXISTS (SELECT 1 FROM [sys].[database_principals] WHERE [type] IN (''E'',''S'',''X'') AND [name] = ' + QUOTENAME(@p1, '''') + ') DROP USER ' + QUOTENAME(@p2);
          EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Deleting User %s: cmd: %s", username, cmd))

	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return err
	}

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

func (m *client) ReadRoleMembership(ctx context.Context, database string, id string) (RoleMembership, error) {
	var roleMembership RoleMembership

	role, member, err := decodeRoleMembershipId(id)
	if err != nil {
		return roleMembership, err
	}

	conn, err := m.getConnForDatabase(database)
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

func (m *client) AssignRole(ctx context.Context, database string, role string, member string) (RoleMembership, error) {
	var roleMembership RoleMembership

	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return roleMembership, err
	}

	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'ALTER ROLE ' + QUOTENAME(@p1) + ' ADD MEMBER ' + QUOTENAME(@p2);
          EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Adding Principal %s to role %s: cmd: %s", member, role, cmd))
	_, err = conn.ExecContext(ctx,
		cmd,
		role,
		member,
	)

	if err != nil {
		return roleMembership, err
	}
	return m.ReadRoleMembership(ctx, database, encodeRoleMembershipId(role, member))
}

func (m *client) UnassignRole(ctx context.Context, database string, role string, principal string) error {
	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return err
	}

	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'ALTER ROLE ' + QUOTENAME(@p1) + ' DROP MEMBER ' + QUOTENAME(@p2);
          EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Removing Principal %s from role %s: cmd: %s", principal, role, cmd))
	_, err = conn.ExecContext(ctx,
		cmd,
		role,
		principal,
	)

	return err
}

func (m *client) ReadDatabasePermission(ctx context.Context, database string, id string) (DatabaseGrantPermission, error) {
	var DatabaseGrantPermission DatabaseGrantPermission
	principal := strings.Split(id, "/")[0]
	permission := strings.Split(id, "/")[1]

	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return DatabaseGrantPermission, err
	}

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

	err = result.Scan(&DatabaseGrantPermission.Principal, &DatabaseGrantPermission.Permission)
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("failed to scan result: %v", err))
		return DatabaseGrantPermission, err
	}

	DatabaseGrantPermission.Id = fmt.Sprintf("%s/%s", DatabaseGrantPermission.Principal, strings.ToLower(DatabaseGrantPermission.Permission))
	tflog.Debug(ctx, fmt.Sprintf("SUCCESS Reading DB permission principal: %s, permission: %s", principal, permission))

	return DatabaseGrantPermission, nil
}

func (m *client) GrantDatabasePermission(ctx context.Context, database string, principal string, permission string) (DatabaseGrantPermission, error) {
	var DatabaseGrantPermission DatabaseGrantPermission

	perm, err := normalizeDatabasePermission(permission)
	if err != nil {
		return DatabaseGrantPermission, err
	}
	principal, err = normalizePrincipalName(principal)
	if err != nil {
		return DatabaseGrantPermission, err
	}

	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return DatabaseGrantPermission, err
	}

	cmd := `DECLARE @sql NVARCHAR(max);
SET @sql = N'GRANT ' + @p1 + N' TO ' + QUOTENAME(@p2) + N';';
EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Granting permission %s to %s", perm, principal))

	_, err = conn.ExecContext(ctx, cmd, perm, principal)
	if err != nil {
		return DatabaseGrantPermission, fmt.Errorf("failed to execute grant query: %v", err)
	}

	return m.ReadDatabasePermission(ctx, database, fmt.Sprintf("%s/%s", principal, perm))
}

func (m *client) RevokeDatabasePermission(ctx context.Context, database string, principal string, permission string) error {
	perm, err := normalizeDatabasePermission(permission)
	if err != nil {
		return err
	}
	principal, err = normalizePrincipalName(principal)
	if err != nil {
		return err
	}

	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return err
	}

	cmd := `DECLARE @sql NVARCHAR(max);
SET @sql = N'REVOKE ' + @p1 + N' FROM ' + QUOTENAME(@p2) + N' CASCADE;';
EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Revoking permission %s from user %s", perm, principal))

	_, err = conn.ExecContext(ctx, cmd, perm, principal)
	if err != nil {
		return fmt.Errorf("failed to execute revoke query: %v", err)
	}

	return nil
}

func normalizePrincipalName(principal string) (string, error) {
	p := strings.TrimSpace(principal)
	if p == "" {
		return "", fmt.Errorf("principal must not be empty")
	}
	// SQL Server principal names are sysname (<= 128 characters).
	if len(p) > 128 {
		return "", fmt.Errorf("principal must be <= 128 characters")
	}
	return p, nil
}

func normalizeDatabasePermission(permission string) (string, error) {
	p := strings.ToUpper(strings.TrimSpace(permission))
	if p == "" {
		return "", fmt.Errorf("permission must not be empty")
	}
	// Permission is interpolated as a keyword into dynamic SQL, so restrict it to safe tokens.
	for _, r := range p {
		if (r >= 'A' && r <= 'Z') || r == '_' || r == ' ' {
			continue
		}
		return "", fmt.Errorf("invalid permission %q: only letters, spaces, and underscores are allowed", permission)
	}
	return p, nil
}

func (m *client) GetRole(ctx context.Context, database string, name string) (Role, error) {
	role := Role{
		Id:   name,
		Name: name,
	}

	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return role, err
	}

	cmd := `SELECT [name] FROM sys.database_principals WHERE [type] = 'R' AND [name] = @name`
	tflog.Debug(ctx, fmt.Sprintf("Executing refresh query for role %s: command %s", name, cmd))
	result := conn.QueryRowContext(ctx, cmd, sql.Named("name", name))
	err = result.Scan(&role.Id)
	return role, err
}

func (m *client) CreateRole(ctx context.Context, database string, name string) (Role, error) {
	var role Role
	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return role, err
	}

	query := fmt.Sprintf("CREATE ROLE [%s]", name)
	_, _ = conn.ExecContext(ctx, query)

	role, err = m.GetRole(ctx, database, name)
	return role, err
}

func (m *client) UpdateRole(ctx context.Context, database string, role Role) (Role, error) {
	var update Role
	// TODO update role.name to update
	update.Id = role.Id
	return m.GetRole(ctx, database, update.Id)
}

func (m *client) DeleteRole(ctx context.Context, database string, name string) error {
	conn, err := m.getConnForDatabase(database)
	if err != nil {
		return err
	}

	query := fmt.Sprintf("DROP ROLE %s", name)
	tflog.Debug(ctx, fmt.Sprintf("Deleting Role %s: cmd: %s", name, query))
	_, err = conn.ExecContext(ctx, query)

	return err
}

func (m *client) GetDatabase(ctx context.Context, name string) (Database, error) {
	var db Database
	cmd := `SELECT [name], [database_id] FROM sys.databases WHERE [name] = @name`
	tflog.Debug(ctx, fmt.Sprintf("Executing refresh query for database %s: command %s", name, cmd))
	result := m.conn.QueryRowContext(ctx, cmd, sql.Named("name", name))
	err := result.Scan(&db.Name, &db.Id)
	return db, err
}

func (m *client) GetDatabaseById(ctx context.Context, id int64) (Database, error) {
	var db Database
	cmd := `SELECT [name], [database_id] FROM sys.databases WHERE [database_id] = @id`
	tflog.Debug(ctx, fmt.Sprintf("Executing refresh query for database %d: command %s", id, cmd))
	result := m.conn.QueryRowContext(ctx, cmd, sql.Named("id", id))
	err := result.Scan(&db.Name, &db.Id)
	return db, err
}

func (m *client) CreateDatabase(ctx context.Context, name string) (Database, error) {
	var db Database
	query := fmt.Sprintf("CREATE DATABASE [%s]", name)
	_, err := m.conn.ExecContext(ctx, query)
	if err != nil {
		return db, fmt.Errorf("failed to create database: %v", err)
	}
	db, err = m.GetDatabase(ctx, name)
	return db, err
}

func (m *client) ExecScript(ctx context.Context, database string, script string) error {
	db := strings.TrimSpace(database)
	if db == "" {
		// No explicit database requested; execute as-is in the current connection context.
		_, err := m.conn.ExecContext(ctx, script)
		if err != nil {
			return fmt.Errorf("failed to execute script: %v", err)
		}
		return nil
	}
	// SQL Server database names are sysname (<= 128 chars).
	if len(db) > 128 {
		return fmt.Errorf("database name must be <= 128 characters")
	}

	cmd := `DECLARE @sql NVARCHAR(max);
SET @sql = N'USE ' + QUOTENAME(@p1) + N'; ' + @p2;
EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Executing script in database %s", db))
	_, err := m.conn.ExecContext(ctx, cmd, db, script)
	if err != nil {
		return fmt.Errorf("failed to execute script in database %s: %v", db, err)
	}

	return nil
}

func validateIdentifier(kind string, value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return fmt.Errorf("invalid %s: must not be empty", kind)
	}
	// SQL Server identifiers are sysname (<= 128 characters).
	if len(v) > 128 {
		return fmt.Errorf("invalid %s: must be <= 128 characters", kind)
	}
	return nil
}

// Login operations.

func (m *client) GetLogin(ctx context.Context, name string) (Login, error) {
	var login Login

	cmd := `SELECT
		p.[name] AS name,
		COALESCE(l.[default_database_name], 'master') AS default_database,
		COALESCE(l.[default_language_name], '') AS default_language,
		p.[is_disabled] AS is_disabled
	FROM sys.server_principals p
	LEFT JOIN sys.sql_logins l ON p.principal_id = l.principal_id
	WHERE p.[name] = @name AND p.[type] IN ('S', 'U', 'G')`

	tflog.Debug(ctx, fmt.Sprintf("Executing query for login %s: %s", name, cmd))
	result := m.conn.QueryRowContext(ctx, cmd, sql.Named("name", name))

	err := result.Scan(&login.Name, &login.DefaultDatabase, &login.DefaultLanguage, &login.IsDisabled)
	return login, err
}

func (m *client) CreateLogin(ctx context.Context, create CreateLogin) (Login, error) {
	var login Login

	if err := validateIdentifier("login name", create.Name); err != nil {
		return login, err
	}
	if create.Password == "" {
		return login, fmt.Errorf("invalid login password: must not be empty")
	}
	if create.DefaultDatabase != "" {
		if err := validateIdentifier("default database", create.DefaultDatabase); err != nil {
			return login, err
		}
	}
	if create.DefaultLanguage != "" {
		if err := validateIdentifier("default language", create.DefaultLanguage); err != nil {
			return login, err
		}
	}

	// Build the CREATE LOGIN command using dynamic SQL for safety.
	var cmdBuilder strings.Builder
	var args []any

	cmdBuilder.WriteString("DECLARE @sql NVARCHAR(max);\n")
	cmdBuilder.WriteString("SET @sql = 'CREATE LOGIN ' + QUOTENAME(@name) + ' WITH PASSWORD = ' + QUOTENAME(@password, '''')")
	args = append(args, sql.Named("name", create.Name))
	args = append(args, sql.Named("password", create.Password))

	if create.DefaultDatabase != "" {
		cmdBuilder.WriteString(" + ', DEFAULT_DATABASE = ' + QUOTENAME(@default_database)")
		args = append(args, sql.Named("default_database", create.DefaultDatabase))
	}
	if create.DefaultLanguage != "" {
		cmdBuilder.WriteString(" + ', DEFAULT_LANGUAGE = ' + QUOTENAME(@default_language)")
		args = append(args, sql.Named("default_language", create.DefaultLanguage))
	}

	cmdBuilder.WriteString(";\n")
	cmdBuilder.WriteString("EXEC (@sql);")

	cmd := cmdBuilder.String()
	tflog.Debug(ctx, fmt.Sprintf("Creating login %s: %s", create.Name, cmd))

	_, err := m.conn.ExecContext(ctx, cmd, args...)
	if err != nil {
		return login, fmt.Errorf("failed to create login: %v", err)
	}

	return m.GetLogin(ctx, create.Name)
}

func (m *client) UpdateLogin(ctx context.Context, update UpdateLogin) (Login, error) {
	if err := validateIdentifier("login name", update.Name); err != nil {
		return Login{}, err
	}
	if update.DefaultDatabase != "" {
		if err := validateIdentifier("default database", update.DefaultDatabase); err != nil {
			return Login{}, err
		}
	}
	if update.DefaultLanguage != "" {
		if err := validateIdentifier("default language", update.DefaultLanguage); err != nil {
			return Login{}, err
		}
	}

	var cmdBuilder strings.Builder
	var args []any

	cmdBuilder.WriteString("DECLARE @sql NVARCHAR(max);\n")
	cmdBuilder.WriteString("SET @sql = 'ALTER LOGIN ' + QUOTENAME(@name)")
	args = append(args, sql.Named("name", update.Name))

	hasChanges := false

	if update.Password != "" {
		cmdBuilder.WriteString(" + ' WITH PASSWORD = ' + QUOTENAME(@password, '''')")
		args = append(args, sql.Named("password", update.Password))
		hasChanges = true
	}

	if update.DefaultDatabase != "" {
		if hasChanges {
			cmdBuilder.WriteString(" + ', DEFAULT_DATABASE = ' + QUOTENAME(@default_database)")
		} else {
			cmdBuilder.WriteString(" + ' WITH DEFAULT_DATABASE = ' + QUOTENAME(@default_database)")
		}
		args = append(args, sql.Named("default_database", update.DefaultDatabase))
		hasChanges = true
	}

	if update.DefaultLanguage != "" {
		if hasChanges {
			cmdBuilder.WriteString(" + ', DEFAULT_LANGUAGE = ' + QUOTENAME(@default_language)")
		} else {
			cmdBuilder.WriteString(" + ' WITH DEFAULT_LANGUAGE = ' + QUOTENAME(@default_language)")
		}
		args = append(args, sql.Named("default_language", update.DefaultLanguage))
		hasChanges = true
	}

	if hasChanges {
		cmdBuilder.WriteString(";\n")
		cmdBuilder.WriteString("EXEC (@sql);")

		cmd := cmdBuilder.String()
		tflog.Debug(ctx, fmt.Sprintf("Updating login %s: %s", update.Name, cmd))

		if _, err := m.conn.ExecContext(ctx, cmd, args...); err != nil {
			return Login{}, fmt.Errorf("failed to update login: %v", err)
		}
	}

	return m.GetLogin(ctx, update.Name)
}

func (m *client) DeleteLogin(ctx context.Context, name string) error {
	if err := validateIdentifier("login name", name); err != nil {
		return err
	}

	cmd := `DECLARE @sql NVARCHAR(max);
SET @sql = 'IF EXISTS (SELECT 1 FROM sys.server_principals WHERE [name] = ' + QUOTENAME(@name, '''') + ' AND [type] IN (''S'', ''U'', ''G'')) DROP LOGIN ' + QUOTENAME(@name);
EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Deleting login %s: %s", name, cmd))
	_, err := m.conn.ExecContext(ctx, cmd, sql.Named("name", name))
	return err
}

// Database options operations

func (m *client) GetDatabaseOptions(ctx context.Context, name string) (DatabaseOptions, error) {
	var opts DatabaseOptions

	cmd := `SELECT
		COALESCE(d.[collation_name], '') AS collation_name,
		d.[compatibility_level],
		d.[recovery_model_desc] AS recovery_model,
		d.[is_read_committed_snapshot_on],
		d.[snapshot_isolation_state] AS allow_snapshot_isolation,
		COALESCE(d.[is_accelerated_database_recovery_on], 0) AS accelerated_database_recovery,
		d.[is_auto_close_on],
		d.[is_auto_shrink_on],
		d.[is_auto_create_stats_on],
		d.[is_auto_update_stats_on],
		d.[is_auto_update_stats_async_on]
	FROM sys.databases d
	WHERE d.[name] = @name`

	tflog.Debug(ctx, fmt.Sprintf("Getting database options for %s", name))
	result := m.conn.QueryRowContext(ctx, cmd, sql.Named("name", name))

	var (
		compLevel            int
		recModel             string
		rcsi                 bool
		snapshotIsolation    int
		adr                  bool
		autoClose            bool
		autoShrink           bool
		autoCreateStats      bool
		autoUpdateStats      bool
		autoUpdateStatsAsync bool
	)

	err := result.Scan(
		&opts.Collation,
		&compLevel,
		&recModel,
		&rcsi,
		&snapshotIsolation,
		&adr,
		&autoClose,
		&autoShrink,
		&autoCreateStats,
		&autoUpdateStats,
		&autoUpdateStatsAsync,
	)
	if err != nil {
		return opts, err
	}

	opts.CompatibilityLevel = &compLevel
	opts.RecoveryModel = &recModel
	opts.ReadCommittedSnapshot = &rcsi
	// snapshot_isolation_state: 0=OFF, 1=ON, 2=IN_TRANSITION_TO_ON, 3=IN_TRANSITION_TO_OFF
	snapshotBool := snapshotIsolation != 0 && snapshotIsolation != 3
	opts.AllowSnapshotIsolation = &snapshotBool
	opts.AcceleratedDatabaseRecovery = &adr
	opts.AutoClose = &autoClose
	opts.AutoShrink = &autoShrink
	opts.AutoCreateStats = &autoCreateStats
	opts.AutoUpdateStats = &autoUpdateStats
	opts.AutoUpdateStatsAsync = &autoUpdateStatsAsync

	return opts, nil
}

func boolToOnOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func validateToken(field, value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	if len(v) > 128 {
		return fmt.Errorf("%s must be <= 128 characters", field)
	}
	for _, r := range v {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return fmt.Errorf("%s contains invalid characters", field)
	}
	return nil
}

func (m *client) execAlterDatabase(ctx context.Context, database string, clause string) error {
	// Use QUOTENAME(@db) to safely inject the database identifier.
	cmd := `DECLARE @sql NVARCHAR(max);
SET @sql = N'ALTER DATABASE ' + QUOTENAME(@db) + N' ` + clause + `';
EXEC (@sql);`

	_, err := m.conn.ExecContext(ctx, cmd, sql.Named("db", database))
	return err
}

func (m *client) SetDatabaseOptions(ctx context.Context, name string, opts DatabaseOptions) error {
	if err := validateIdentifier("database name", name); err != nil {
		return err
	}

	var errs []string

	if opts.Collation != "" {
		// Collation is an identifier-like token.
		if err := validateToken("collation", opts.Collation); err != nil {
			return err
		}
		stmt := "COLLATE " + opts.Collation
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: ALTER DATABASE %s %s", name, stmt))
		if err := m.execAlterDatabase(ctx, name, stmt); err != nil {
			errs = append(errs, fmt.Sprintf("COLLATE: %v", err))
		}
	}

	if opts.CompatibilityLevel != nil {
		stmt := fmt.Sprintf("SET COMPATIBILITY_LEVEL = %d", *opts.CompatibilityLevel)
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: ALTER DATABASE %s %s", name, stmt))
		if err := m.execAlterDatabase(ctx, name, stmt); err != nil {
			errs = append(errs, fmt.Sprintf("COMPATIBILITY_LEVEL: %v", err))
		}
	}

	if opts.RecoveryModel != nil && *opts.RecoveryModel != "" {
		model := strings.ToUpper(strings.TrimSpace(*opts.RecoveryModel))
		switch model {
		case "FULL", "BULK_LOGGED", "SIMPLE":
			// ok
		default:
			return fmt.Errorf("invalid recovery_model %q", *opts.RecoveryModel)
		}
		stmt := "SET RECOVERY " + model
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: ALTER DATABASE %s %s", name, stmt))
		if err := m.execAlterDatabase(ctx, name, stmt); err != nil {
			errs = append(errs, fmt.Sprintf("RECOVERY: %v", err))
		}
	}

	if opts.AllowSnapshotIsolation != nil {
		stmt := "SET ALLOW_SNAPSHOT_ISOLATION " + boolToOnOff(*opts.AllowSnapshotIsolation)
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: ALTER DATABASE %s %s", name, stmt))
		if err := m.execAlterDatabase(ctx, name, stmt); err != nil {
			errs = append(errs, fmt.Sprintf("ALLOW_SNAPSHOT_ISOLATION: %v", err))
		}
	}

	if opts.ReadCommittedSnapshot != nil {
		stmt := "SET READ_COMMITTED_SNAPSHOT " + boolToOnOff(*opts.ReadCommittedSnapshot) + " WITH ROLLBACK IMMEDIATE"
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: ALTER DATABASE %s %s", name, stmt))
		if err := m.execAlterDatabase(ctx, name, stmt); err != nil {
			errs = append(errs, fmt.Sprintf("READ_COMMITTED_SNAPSHOT: %v", err))
		}
	}

	if opts.AutoClose != nil {
		stmt := "SET AUTO_CLOSE " + boolToOnOff(*opts.AutoClose)
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: ALTER DATABASE %s %s", name, stmt))
		if err := m.execAlterDatabase(ctx, name, stmt); err != nil {
			errs = append(errs, fmt.Sprintf("AUTO_CLOSE: %v", err))
		}
	}

	if opts.AutoShrink != nil {
		stmt := "SET AUTO_SHRINK " + boolToOnOff(*opts.AutoShrink)
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: ALTER DATABASE %s %s", name, stmt))
		if err := m.execAlterDatabase(ctx, name, stmt); err != nil {
			errs = append(errs, fmt.Sprintf("AUTO_SHRINK: %v", err))
		}
	}

	if opts.AutoCreateStats != nil {
		stmt := "SET AUTO_CREATE_STATISTICS " + boolToOnOff(*opts.AutoCreateStats)
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: ALTER DATABASE %s %s", name, stmt))
		if err := m.execAlterDatabase(ctx, name, stmt); err != nil {
			errs = append(errs, fmt.Sprintf("AUTO_CREATE_STATISTICS: %v", err))
		}
	}

	if opts.AutoUpdateStats != nil {
		stmt := "SET AUTO_UPDATE_STATISTICS " + boolToOnOff(*opts.AutoUpdateStats)
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: ALTER DATABASE %s %s", name, stmt))
		if err := m.execAlterDatabase(ctx, name, stmt); err != nil {
			errs = append(errs, fmt.Sprintf("AUTO_UPDATE_STATISTICS: %v", err))
		}
	}

	if opts.AutoUpdateStatsAsync != nil {
		stmt := "SET AUTO_UPDATE_STATISTICS_ASYNC " + boolToOnOff(*opts.AutoUpdateStatsAsync)
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: ALTER DATABASE %s %s", name, stmt))
		if err := m.execAlterDatabase(ctx, name, stmt); err != nil {
			errs = append(errs, fmt.Sprintf("AUTO_UPDATE_STATISTICS_ASYNC: %v", err))
		}
	}

	if opts.AcceleratedDatabaseRecovery != nil {
		stmt := "SET ACCELERATED_DATABASE_RECOVERY = " + boolToOnOff(*opts.AcceleratedDatabaseRecovery) + " WITH ROLLBACK IMMEDIATE"
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: ALTER DATABASE %s %s", name, stmt))
		if err := m.execAlterDatabase(ctx, name, stmt); err != nil {
			errs = append(errs, fmt.Sprintf("ACCELERATED_DATABASE_RECOVERY: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to set database options: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *client) GetDatabaseScopedConfigurations(ctx context.Context, name string) ([]DatabaseScopedConfiguration, error) {
	var configs []DatabaseScopedConfiguration

	conn, err := m.getConnForDatabase(name)
	if err != nil {
		return configs, err
	}

	cmd := `SELECT
		[name],
		CASE SQL_VARIANT_PROPERTY([value], 'BaseType')
			WHEN 'binary' THEN CONVERT(NVARCHAR(MAX), CONVERT(VARBINARY(MAX), [value]), 1)
			ELSE CONVERT(NVARCHAR(MAX), [value])
		END AS value,
		CASE SQL_VARIANT_PROPERTY([value_for_secondary], 'BaseType')
			WHEN 'binary' THEN CONVERT(NVARCHAR(MAX), CONVERT(VARBINARY(MAX), [value_for_secondary]), 1)
			ELSE CONVERT(NVARCHAR(MAX), [value_for_secondary])
		END AS value_for_secondary
	FROM sys.database_scoped_configurations
	WHERE [value] IS NOT NULL`

	tflog.Debug(ctx, fmt.Sprintf("Getting database scoped configurations for %s", name))
	rows, err := conn.QueryContext(ctx, cmd)
	if err != nil {
		return configs, err
	}
	defer rows.Close()

	for rows.Next() {
		var cfg DatabaseScopedConfiguration
		var valueForSecondary sql.NullString
		if err := rows.Scan(&cfg.Name, &cfg.Value, &valueForSecondary); err != nil {
			return configs, err
		}
		if valueForSecondary.Valid {
			cfg.ValueForSecondary = valueForSecondary.String
		}
		configs = append(configs, cfg)
	}

	return configs, rows.Err()
}

func (m *client) SetDatabaseScopedConfiguration(ctx context.Context, name string, cfg DatabaseScopedConfiguration) error {
	if err := validateIdentifier("database name", name); err != nil {
		return err
	}
	if err := validateToken("scoped configuration name", cfg.Name); err != nil {
		return err
	}
	if err := validateToken("scoped configuration value", cfg.Value); err != nil {
		return err
	}
	if cfg.ValueForSecondary != "" {
		if err := validateToken("scoped configuration value_for_secondary", cfg.ValueForSecondary); err != nil {
			return err
		}
	}

	conn, err := m.getConnForDatabase(name)
	if err != nil {
		return err
	}

	cmd := fmt.Sprintf("ALTER DATABASE SCOPED CONFIGURATION SET %s = %s", cfg.Name, cfg.Value)
	tflog.Debug(ctx, fmt.Sprintf("Setting database scoped configuration: %s", cmd))
	if _, err = conn.ExecContext(ctx, cmd); err != nil {
		return err
	}

	if cfg.ValueForSecondary != "" {
		cmd = fmt.Sprintf("ALTER DATABASE SCOPED CONFIGURATION FOR SECONDARY SET %s = %s", cfg.Name, cfg.ValueForSecondary)
		tflog.Debug(ctx, fmt.Sprintf("Setting database scoped configuration for secondary: %s", cmd))
		if _, err = conn.ExecContext(ctx, cmd); err != nil {
			return err
		}
	}

	return nil
}

func (m *client) ClearDatabaseScopedConfiguration(ctx context.Context, name string, configName string) error {
	if err := validateIdentifier("database name", name); err != nil {
		return err
	}
	if err := validateToken("scoped configuration name", configName); err != nil {
		return err
	}

	conn, err := m.getConnForDatabase(name)
	if err != nil {
		return err
	}

	cmd := fmt.Sprintf("ALTER DATABASE SCOPED CONFIGURATION CLEAR %s", configName)
	tflog.Debug(ctx, fmt.Sprintf("Clearing database scoped configuration: %s", cmd))
	_, err = conn.ExecContext(ctx, cmd)
	return err
}
