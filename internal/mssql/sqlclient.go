package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	_ "github.com/microsoft/go-mssqldb"
)

type client struct {
	conn     *sql.DB
	host     string
	port     int64
	database string
	username string
	password string
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
		panic(err)
	}

	return client{
		conn:     conn,
		host:     host,
		port:     port,
		database: database,
		username: username,
		password: password,
	}
}

// getConnForDatabase returns a connection to the specified database.
// If database is empty or matches the client's default database, returns the existing connection.
// Otherwise, creates a new connection to the target database.
// The caller must close the connection if closeConn is true.
func (m client) getConnForDatabase(database string) (conn *sql.DB, closeConn bool, err error) {
	if database == "" || database == m.database {
		return m.conn, false, nil
	}

	newConn, err := sql.Open("sqlserver", buildConnString(m.host, m.port, database, m.username, m.password))
	if err != nil {
		return nil, false, fmt.Errorf("failed to connect to database %s: %v", database, err)
	}

	if err := newConn.Ping(); err != nil {
		newConn.Close()
		return nil, false, fmt.Errorf("failed to ping database %s: %v", database, err)
	}

	return newConn, true, nil
}

// User operations - database parameter specifies target database (empty = provider's default)

func (m client) GetUser(ctx context.Context, database string, username string) (User, error) {
	conn, closeConn, err := m.getConnForDatabase(database)
	if err != nil {
		return User{}, err
	}
	if closeConn {
		defer conn.Close()
	}

	return m.getUserWithConn(ctx, conn, username)
}

func (m client) CreateUser(ctx context.Context, database string, create CreateUser) (User, error) {
	conn, closeConn, err := m.getConnForDatabase(database)
	if err != nil {
		return User{}, err
	}
	if closeConn {
		defer conn.Close()
	}

	return m.createUserWithConn(ctx, conn, create)
}

func buildCreateUser(create CreateUser) (string, []any, error) {
	var cmdBuilder strings.Builder
	var optionsBuilder strings.Builder

	var args []any

	// Login-based user validations
	if create.LoginName != "" && create.Password != "" {
		return "", nil, fmt.Errorf("invalid user %s, login-based users may not have passwords (password is on the login)", create.Username)
	}

	if create.LoginName != "" && create.External {
		return "", nil, fmt.Errorf("invalid user %s, login-based users cannot be external", create.Username)
	}

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

	// Handle login-based user vs contained user vs external user
	if create.LoginName != "" {
		// Login-based user: CREATE USER [username] FOR LOGIN [login_name]
		cmdBuilder.WriteString(" + ' FOR LOGIN ' + QUOTENAME(@login_name)")
		args = append(args, sql.Named("login_name", create.LoginName))
	} else if create.External {
		// External user (Azure AD): CREATE USER [username] FROM EXTERNAL PROVIDER
		cmdBuilder.WriteString(" + ' FROM EXTERNAL PROVIDER '")
	}
	// Else: contained user with password (handled in options below)

	// Begin Options. Easy since we make DefaultSchema required
	addOption(&optionsBuilder, &args, "DEFAULT_SCHEMA", create.DefaultSchema, true)
	// Only add password for contained users (not login-based, not external)
	if create.LoginName == "" && !create.External {
		addOption(&optionsBuilder, &args, "PASSWORD", create.Password, false)
	}
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

func (m client) UpdateUser(ctx context.Context, database string, update UpdateUser) (User, error) {
	conn, closeConn, err := m.getConnForDatabase(database)
	if err != nil {
		return User{}, err
	}
	if closeConn {
		defer conn.Close()
	}

	return m.updateUserWithConn(ctx, conn, update)
}

func (m client) DeleteUser(ctx context.Context, database string, username string) error {
	conn, closeConn, err := m.getConnForDatabase(database)
	if err != nil {
		return err
	}
	if closeConn {
		defer conn.Close()
	}

	return m.deleteUserWithConn(ctx, conn, username)
}

func (m client) getUserWithConn(ctx context.Context, conn *sql.DB, username string) (User, error) {
	user := User{Id: username}

	cmd := `SELECT
    P.[name] AS id,
    COALESCE(CONVERT(varchar(175), P.[sid],1), '') AS sid,
    P.[name] AS name,
    P.[type] AS type,
    CASE WHEN P.[type] IN ('E', 'X') THEN 1 ELSE 0 END AS ext,
    COALESCE(P.[default_schema_name], '') AS default_schema_name
FROM sys.database_principals P
WHERE P.[name] = @username`

	result := conn.QueryRowContext(ctx, cmd, sql.Named("username", username))
	err := result.Scan(&user.Id, &user.Sid, &user.Username, &user.Type, &user.External, &user.DefaultSchema)
	return user, err
}

func (m client) createUserWithConn(ctx context.Context, conn *sql.DB, create CreateUser) (User, error) {
	var user User
	cmd, args, err := buildCreateUser(create)
	if err != nil {
		return user, err
	}

	_, err = conn.ExecContext(ctx, cmd, args...)
	if err != nil {
		return user, err
	}

	return m.getUserWithConn(ctx, conn, create.Username)
}

func (m client) updateUserWithConn(ctx context.Context, conn *sql.DB, update UpdateUser) (User, error) {
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

		_, err := conn.ExecContext(ctx, cmd, args...)
		if err != nil {
			return User{}, err
		}
	}

	return m.getUserWithConn(ctx, conn, update.Id)
}

func (m client) deleteUserWithConn(ctx context.Context, conn *sql.DB, username string) error {
	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'IF EXISTS (SELECT 1 FROM [sys].[database_principals] WHERE [type] IN (''E'',''S'',''X'') AND [name] = ' + QUOTENAME(@p1, '''') + ') DROP USER ' + QUOTENAME(@p2);
          EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Deleting User %s: cmd: %s", username, cmd))
	_, err := conn.ExecContext(ctx, cmd, username, username)
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

// Database role membership operations

func (m client) ReadRoleMembership(ctx context.Context, database string, role string, principal string) (RoleMembership, error) {
	var roleMembership RoleMembership

	conn, closeConn, err := m.getConnForDatabase(database)
	if err != nil {
		return roleMembership, err
	}
	if closeConn {
		defer conn.Close()
	}

	cmd := `SELECT r.name role_principal_name, m.name AS member_principal_name
FROM sys.database_role_members rm
JOIN sys.database_principals r ON rm.role_principal_id = r.principal_id
JOIN sys.database_principals m ON rm.member_principal_id = m.principal_id
WHERE r.type = 'R' AND r.name = @role AND m.name = @principal`

	tflog.Debug(ctx, fmt.Sprintf("Reading Role Assignment role %s, member %s", role, principal))

	result := conn.QueryRowContext(ctx, cmd,
		sql.Named("role", role),
		sql.Named("principal", principal),
	)

	err = result.Scan(&roleMembership.Role, &roleMembership.Member)
	if err != nil {
		return roleMembership, err
	}

	roleMembership.Id = encodeRoleMembershipId(roleMembership.Role, roleMembership.Member)
	return roleMembership, nil
}

func (m client) AssignRole(ctx context.Context, database string, role string, member string) (RoleMembership, error) {
	var roleMembership RoleMembership

	conn, closeConn, err := m.getConnForDatabase(database)
	if err != nil {
		return roleMembership, err
	}
	if closeConn {
		defer conn.Close()
	}

	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'ALTER ROLE ' + QUOTENAME(@role) + ' ADD MEMBER ' + QUOTENAME(@member);
          EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Adding Principal %s to role %s", member, role))
	_, err = conn.ExecContext(ctx, cmd,
		sql.Named("role", role),
		sql.Named("member", member),
	)

	if err != nil {
		return roleMembership, err
	}
	return m.ReadRoleMembership(ctx, database, role, member)
}

func (m client) UnassignRole(ctx context.Context, database string, role string, principal string) error {
	conn, closeConn, err := m.getConnForDatabase(database)
	if err != nil {
		return err
	}
	if closeConn {
		defer conn.Close()
	}

	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'ALTER ROLE ' + QUOTENAME(@role) + ' DROP MEMBER ' + QUOTENAME(@principal);
          EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Removing Principal %s from role %s", principal, role))
	_, err = conn.ExecContext(ctx, cmd,
		sql.Named("role", role),
		sql.Named("principal", principal),
	)

	return err
}

// Server role operations

func (m client) ReadServerRoleMembership(ctx context.Context, role string, principal string) (RoleMembership, error) {
	var roleMembership RoleMembership

	cmd := `SELECT r.name AS role_name, m.name AS member_name
FROM sys.server_role_members rm
JOIN sys.server_principals r ON rm.role_principal_id = r.principal_id
JOIN sys.server_principals m ON rm.member_principal_id = m.principal_id
WHERE r.name = @role AND m.name = @principal`

	tflog.Debug(ctx, fmt.Sprintf("Reading Server Role Assignment role %s, member %s", role, principal))

	result := m.conn.QueryRowContext(ctx, cmd,
		sql.Named("role", role),
		sql.Named("principal", principal),
	)

	err := result.Scan(&roleMembership.Role, &roleMembership.Member)
	if err != nil {
		return roleMembership, err
	}

	roleMembership.Id = encodeRoleMembershipId(roleMembership.Role, roleMembership.Member)
	return roleMembership, nil
}

func (m client) AssignServerRole(ctx context.Context, role string, principal string) (RoleMembership, error) {
	var roleMembership RoleMembership

	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'ALTER SERVER ROLE ' + QUOTENAME(@role) + ' ADD MEMBER ' + QUOTENAME(@principal);
          EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Adding Principal %s to server role %s", principal, role))
	_, err := m.conn.ExecContext(ctx, cmd,
		sql.Named("role", role),
		sql.Named("principal", principal),
	)

	if err != nil {
		return roleMembership, err
	}
	return m.ReadServerRoleMembership(ctx, role, principal)
}

func (m client) UnassignServerRole(ctx context.Context, role string, principal string) error {
	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'ALTER SERVER ROLE ' + QUOTENAME(@role) + ' DROP MEMBER ' + QUOTENAME(@principal);
          EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Removing Principal %s from server role %s", principal, role))
	_, err := m.conn.ExecContext(ctx, cmd,
		sql.Named("role", role),
		sql.Named("principal", principal),
	)

	return err
}

// Permission operations

func encodePermissionId(grant GrantPermission) string {
	// Format: database/principal/permission/objecttype/objectname (last two optional)
	db := grant.Database
	if db == "" {
		db = "default"
	}
	id := fmt.Sprintf("%s/%s/%s", db, grant.Principal, grant.Permission)
	if grant.ObjectType != "" {
		id += "/" + grant.ObjectType
		if grant.ObjectName != "" {
			id += "/" + grant.ObjectName
		}
	}
	return id
}

func (m client) ReadPermission(ctx context.Context, grant GrantPermission) (GrantPermission, error) {
	conn, closeConn, err := m.getConnForDatabase(grant.Database)
	if err != nil {
		return grant, err
	}
	if closeConn {
		defer conn.Close()
	}

	var cmd string
	var result *sql.Row

	if grant.ObjectType != "" && grant.ObjectName != "" {
		// Object-level permission query
		// Note: class=1 is OBJECT_OR_COLUMN, class=3 is SCHEMA
		// We normalize class_desc to our standard names: SCHEMA or OBJECT
		objSchema, objName := splitSchemaObject(grant.ObjectName)
		cmd = `
			SELECT
				dp.[name] AS [principal],
				sdp.[permission_name] AS [permission],
				CASE sdp.[class]
					WHEN 3 THEN 'SCHEMA'
					WHEN 1 THEN 'OBJECT'
					ELSE sdp.[class_desc]
				END AS [object_type],
				COALESCE(OBJECT_SCHEMA_NAME(sdp.[major_id]), '') AS [object_schema],
				CASE 
					WHEN sdp.[class] = 3 THEN SCHEMA_NAME(sdp.[major_id])
					ELSE OBJECT_NAME(sdp.[major_id])
				END AS [object_name]
			FROM
				sys.database_permissions AS sdp
			JOIN
				sys.database_principals AS dp ON sdp.grantee_principal_id = dp.principal_id
			WHERE
				sdp.[state] IN ('G', 'W')
				AND dp.[name] = @principal
				AND sdp.[permission_name] = @permission
				AND (
					(sdp.[class] = 1 AND OBJECT_NAME(sdp.[major_id]) = @object_name AND (@object_schema = '' OR OBJECT_SCHEMA_NAME(sdp.[major_id]) = @object_schema))
					OR (sdp.[class] = 3 AND SCHEMA_NAME(sdp.[major_id]) = @object_name)
				)`
		result = conn.QueryRowContext(ctx, cmd,
			sql.Named("principal", grant.Principal),
			sql.Named("permission", grant.Permission),
			sql.Named("object_name", objName),
			sql.Named("object_schema", objSchema),
		)
	} else {
		// Database-level permission query
		cmd = `
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
				AND dp.[name] = @principal
				AND sdp.[permission_name] = @permission`
		result = conn.QueryRowContext(ctx, cmd,
			sql.Named("principal", grant.Principal),
			sql.Named("permission", grant.Permission),
		)
	}

	tflog.Debug(ctx, fmt.Sprintf("Reading permission: %s", cmd))

	var objType, objSchema, objName string
	if grant.ObjectType != "" {
		err := result.Scan(&grant.Principal, &grant.Permission, &objType, &objSchema, &objName)
		if err != nil {
			return grant, err
		}
		// Preserve caller-specified type for OBJECT class (TABLE/VIEW/PROC/FUNCTION)
		if strings.EqualFold(objType, "OBJECT") && grant.ObjectType != "" && !strings.EqualFold(grant.ObjectType, "OBJECT") {
			// keep existing grant.ObjectType
		} else {
			grant.ObjectType = objType
		}
		if strings.EqualFold(grant.ObjectType, "SCHEMA") {
			// For schema grants, use schema name only
			grant.ObjectName = objName
		} else if objSchema != "" {
			grant.ObjectName = fmt.Sprintf("%s.%s", objSchema, objName)
		} else {
			grant.ObjectName = objName
		}
	} else {
		err := result.Scan(&grant.Principal, &grant.Permission)
		if err != nil {
			return grant, err
		}
	}

	grant.Id = encodePermissionId(grant)
	return grant, nil
}

// normalizeObjectType converts user-friendly object types to SQL Server securable class names
// Valid inputs: SCHEMA, OBJECT, TABLE, VIEW, PROCEDURE, FUNCTION
// SQL Server only recognizes SCHEMA and OBJECT as securable classes for ON clause
func normalizeObjectType(objectType string) string {
	switch strings.ToUpper(objectType) {
	case "SCHEMA":
		return "SCHEMA"
	case "OBJECT", "TABLE", "VIEW", "PROCEDURE", "FUNCTION":
		return "OBJECT"
	default:
		return objectType // Pass through as-is for other types
	}
}

// splitSchemaObject splits "schema.object" into schema + object.
// If no schema is provided, schema is returned as empty string.
func splitSchemaObject(name string) (schema string, object string) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", name
}

func (m client) GrantPermission(ctx context.Context, grant GrantPermission) (GrantPermission, error) {
	// Get connection to target database
	conn, closeConn, err := m.getConnForDatabase(grant.Database)
	if err != nil {
		return grant, err
	}
	if closeConn {
		defer conn.Close()
	}

	var query string
	if grant.ObjectType != "" && grant.ObjectName != "" {
		// Object-level grant: GRANT permission ON securable_class::[objectname] TO [principal]
		// Normalize object type to valid SQL Server securable class (SCHEMA or OBJECT)
		securableClass := normalizeObjectType(grant.ObjectType)
		objSchema, objName := splitSchemaObject(grant.ObjectName)
		if objSchema != "" {
			query = fmt.Sprintf("GRANT %s ON %s::[%s].[%s] TO [%s]",
				grant.Permission, securableClass, objSchema, objName, grant.Principal)
		} else {
			query = fmt.Sprintf("GRANT %s ON %s::[%s] TO [%s]",
				grant.Permission, securableClass, objName, grant.Principal)
		}
	} else {
		// Database-level grant
		query = fmt.Sprintf("GRANT %s TO [%s]", grant.Permission, grant.Principal)
	}

	tflog.Debug(ctx, fmt.Sprintf("Granting permission: %s", query))

	_, err = conn.ExecContext(ctx, query)
	if err != nil {
		return grant, fmt.Errorf("failed to execute grant: %v", err)
	}

	// Store normalized object type in the result
	grant.Id = encodePermissionId(grant)
	return grant, nil
}

func (m client) RevokePermission(ctx context.Context, grant GrantPermission) error {
	// Get connection to target database
	conn, closeConn, err := m.getConnForDatabase(grant.Database)
	if err != nil {
		return err
	}
	if closeConn {
		defer conn.Close()
	}

	var query string
	if grant.ObjectType != "" && grant.ObjectName != "" {
		// Object-level revoke with normalized securable class
		securableClass := normalizeObjectType(grant.ObjectType)
		objSchema, objName := splitSchemaObject(grant.ObjectName)
		if objSchema != "" {
			query = fmt.Sprintf("REVOKE %s ON %s::[%s].[%s] FROM [%s] CASCADE",
				grant.Permission, securableClass, objSchema, objName, grant.Principal)
		} else {
			query = fmt.Sprintf("REVOKE %s ON %s::[%s] FROM [%s] CASCADE",
				grant.Permission, securableClass, objName, grant.Principal)
		}
	} else {
		// Database-level revoke
		query = fmt.Sprintf("REVOKE %s FROM [%s] CASCADE", grant.Permission, grant.Principal)
	}

	tflog.Debug(ctx, fmt.Sprintf("Revoking permission: %s", query))

	_, err = conn.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to execute revoke: %v", err)
	}

	return nil
}

// Database role management

func (m client) GetRole(ctx context.Context, database string, name string) (Role, error) {
	conn, closeConn, err := m.getConnForDatabase(database)
	if err != nil {
		return Role{}, err
	}
	if closeConn {
		defer conn.Close()
	}

	role := Role{Id: name, Name: name}
	cmd := `SELECT [name] FROM sys.database_principals WHERE [type] = 'R' AND [name] = @name`
	tflog.Debug(ctx, fmt.Sprintf("Executing query for role %s", name))
	result := conn.QueryRowContext(ctx, cmd, sql.Named("name", name))
	err = result.Scan(&role.Id)
	return role, err
}

func (m client) CreateRole(ctx context.Context, database string, name string) (Role, error) {
	conn, closeConn, err := m.getConnForDatabase(database)
	if err != nil {
		return Role{}, err
	}
	if closeConn {
		defer conn.Close()
	}

	query := fmt.Sprintf("CREATE ROLE [%s]", name)
	if _, err := conn.ExecContext(ctx, query); err != nil {
		return Role{}, err
	}

	return m.GetRole(ctx, database, name)
}

func (m client) UpdateRole(ctx context.Context, database string, role Role) (Role, error) {
	// TODO: implement role rename if needed
	return m.GetRole(ctx, database, role.Id)
}

func (m client) DeleteRole(ctx context.Context, database string, name string) error {
	conn, closeConn, err := m.getConnForDatabase(database)
	if err != nil {
		return err
	}
	if closeConn {
		defer conn.Close()
	}

	query := fmt.Sprintf("DROP ROLE [%s]", name)
	tflog.Debug(ctx, fmt.Sprintf("Deleting Role %s", name))
	_, err = conn.ExecContext(ctx, query)
	return err
}

func (m client) GetDatabase(ctx context.Context, name string) (Database, error) {
	var db Database
	cmd := `SELECT [name], [database_id] FROM sys.databases WHERE [name] = @name`
	tflog.Debug(ctx, fmt.Sprintf("Getting database %s", name))
	result := m.conn.QueryRowContext(ctx, cmd, sql.Named("name", name))
	err := result.Scan(&db.Name, &db.Id)
	return db, err
}

func (m client) CreateDatabase(ctx context.Context, name string, collation string) (Database, error) {
	var db Database
	var query string
	if collation != "" {
		query = fmt.Sprintf("CREATE DATABASE [%s] COLLATE %s", name, collation)
	} else {
		query = fmt.Sprintf("CREATE DATABASE [%s]", name)
	}

	if _, err := m.conn.ExecContext(ctx, query); err != nil {
		return db, fmt.Errorf("failed to create database: %v", err)
	}
	db, err := m.GetDatabase(ctx, name)
	return db, err
}

// Login operations

func (m client) GetLogin(ctx context.Context, name string) (Login, error) {
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

func (m client) CreateLogin(ctx context.Context, create CreateLogin) (Login, error) {
	var login Login

	// Build the CREATE LOGIN command using dynamic SQL for safety
	var cmdBuilder strings.Builder
	var args []any

	cmdBuilder.WriteString("DECLARE @sql NVARCHAR(max);\n")
	cmdBuilder.WriteString("SET @sql = 'CREATE LOGIN ' + QUOTENAME(@name) + ' WITH PASSWORD = ' + QUOTENAME(@password, '''')")
	args = append(args, sql.Named("name", create.Name))
	args = append(args, sql.Named("password", create.Password))

	// Add default database if specified
	if create.DefaultDatabase != "" {
		cmdBuilder.WriteString(" + ', DEFAULT_DATABASE = ' + QUOTENAME(@default_database)")
		args = append(args, sql.Named("default_database", create.DefaultDatabase))
	}

	// Add default language if specified
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

func (m client) UpdateLogin(ctx context.Context, update UpdateLogin) (Login, error) {
	var cmdBuilder strings.Builder
	var args []any

	cmdBuilder.WriteString("DECLARE @sql NVARCHAR(max);\n")
	cmdBuilder.WriteString("SET @sql = 'ALTER LOGIN ' + QUOTENAME(@name)")
	args = append(args, sql.Named("name", update.Name))

	hasChanges := false

	// Update password if specified
	if update.Password != "" {
		cmdBuilder.WriteString(" + ' WITH PASSWORD = ' + QUOTENAME(@password, '''')")
		args = append(args, sql.Named("password", update.Password))
		hasChanges = true
	}

	// Update default database if specified
	if update.DefaultDatabase != "" {
		if hasChanges {
			cmdBuilder.WriteString(" + ', DEFAULT_DATABASE = ' + QUOTENAME(@default_database)")
		} else {
			cmdBuilder.WriteString(" + ' WITH DEFAULT_DATABASE = ' + QUOTENAME(@default_database)")
		}
		args = append(args, sql.Named("default_database", update.DefaultDatabase))
		hasChanges = true
	}

	// Update default language if specified
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

		_, err := m.conn.ExecContext(ctx, cmd, args...)
		if err != nil {
			return Login{}, fmt.Errorf("failed to update login: %v", err)
		}
	}

	return m.GetLogin(ctx, update.Name)
}

func (m client) DeleteLogin(ctx context.Context, name string) error {
	cmd := `DECLARE @sql NVARCHAR(max);
          SET @sql = 'IF EXISTS (SELECT 1 FROM sys.server_principals WHERE [name] = ' + QUOTENAME(@name, '''') + ' AND [type] IN (''S'', ''U'', ''G'')) DROP LOGIN ' + QUOTENAME(@name);
          EXEC (@sql);`

	tflog.Debug(ctx, fmt.Sprintf("Deleting login %s: %s", name, cmd))
	_, err := m.conn.ExecContext(ctx, cmd, sql.Named("name", name))

	return err
}

// Database options operations

func (m client) GetDatabaseOptions(ctx context.Context, name string) (DatabaseOptions, error) {
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

	// Scan into temporary variables
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

	// Assign pointer values
	opts.CompatibilityLevel = &compLevel
	opts.RecoveryModel = &recModel
	opts.ReadCommittedSnapshot = &rcsi
	// snapshot_isolation_state: 0=OFF, 1=ON, 2=IN_TRANSITION_TO_ON, 3=IN_TRANSITION_TO_OFF
	// Treat any non-zero and not transitioning to OFF as enabled to avoid transient drift.
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

func (m client) SetDatabaseOptions(ctx context.Context, name string, opts DatabaseOptions) error {
	// Build ALTER DATABASE statements for each option
	// Note: Collation cannot be changed after database creation - it must be set at CREATE DATABASE time
	// Note: RCSI and snapshot isolation use WITH ROLLBACK IMMEDIATE to handle exclusive access requirement

	var errors []string

	// Compatibility level (if specified)
	if opts.CompatibilityLevel != nil {
		stmt := fmt.Sprintf("ALTER DATABASE [%s] SET COMPATIBILITY_LEVEL = %d", name, *opts.CompatibilityLevel)
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: %s", stmt))
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			errors = append(errors, fmt.Sprintf("COMPATIBILITY_LEVEL: %v", err))
		}
	}

	// Recovery model
	if opts.RecoveryModel != nil && *opts.RecoveryModel != "" {
		stmt := fmt.Sprintf("ALTER DATABASE [%s] SET RECOVERY %s", name, *opts.RecoveryModel)
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: %s", stmt))
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			errors = append(errors, fmt.Sprintf("RECOVERY: %v", err))
		}
	}

	// Snapshot isolation - uses WITH ROLLBACK IMMEDIATE for exclusive access
	if opts.AllowSnapshotIsolation != nil {
		// NOTE: WITH ROLLBACK IMMEDIATE is not supported for snapshot isolation state changes
		stmt := fmt.Sprintf("ALTER DATABASE [%s] SET ALLOW_SNAPSHOT_ISOLATION %s", name, boolToOnOff(*opts.AllowSnapshotIsolation))
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: %s", stmt))
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			errors = append(errors, fmt.Sprintf("ALLOW_SNAPSHOT_ISOLATION: %v", err))
		}
	}

	// Read committed snapshot - uses WITH ROLLBACK IMMEDIATE to terminate active connections
	// This is required because RCSI changes need exclusive access to the database
	if opts.ReadCommittedSnapshot != nil {
		stmt := fmt.Sprintf("ALTER DATABASE [%s] SET READ_COMMITTED_SNAPSHOT %s WITH ROLLBACK IMMEDIATE", name, boolToOnOff(*opts.ReadCommittedSnapshot))
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: %s", stmt))
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			errors = append(errors, fmt.Sprintf("READ_COMMITTED_SNAPSHOT: %v", err))
		}
	}

	// Auto options - only apply if explicitly set
	if opts.AutoClose != nil {
		stmt := fmt.Sprintf("ALTER DATABASE [%s] SET AUTO_CLOSE %s", name, boolToOnOff(*opts.AutoClose))
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: %s", stmt))
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			errors = append(errors, fmt.Sprintf("AUTO_CLOSE: %v", err))
		}
	}

	if opts.AutoShrink != nil {
		stmt := fmt.Sprintf("ALTER DATABASE [%s] SET AUTO_SHRINK %s", name, boolToOnOff(*opts.AutoShrink))
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: %s", stmt))
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			errors = append(errors, fmt.Sprintf("AUTO_SHRINK: %v", err))
		}
	}

	if opts.AutoCreateStats != nil {
		stmt := fmt.Sprintf("ALTER DATABASE [%s] SET AUTO_CREATE_STATISTICS %s", name, boolToOnOff(*opts.AutoCreateStats))
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: %s", stmt))
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			errors = append(errors, fmt.Sprintf("AUTO_CREATE_STATISTICS: %v", err))
		}
	}

	if opts.AutoUpdateStats != nil {
		stmt := fmt.Sprintf("ALTER DATABASE [%s] SET AUTO_UPDATE_STATISTICS %s", name, boolToOnOff(*opts.AutoUpdateStats))
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: %s", stmt))
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			errors = append(errors, fmt.Sprintf("AUTO_UPDATE_STATISTICS: %v", err))
		}
	}

	if opts.AutoUpdateStatsAsync != nil {
		stmt := fmt.Sprintf("ALTER DATABASE [%s] SET AUTO_UPDATE_STATISTICS_ASYNC %s", name, boolToOnOff(*opts.AutoUpdateStatsAsync))
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: %s", stmt))
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			errors = append(errors, fmt.Sprintf("AUTO_UPDATE_STATISTICS_ASYNC: %v", err))
		}
	}

	// Accelerated database recovery (SQL Server 2019+, Azure SQL)
	if opts.AcceleratedDatabaseRecovery != nil {
		stmt := fmt.Sprintf("ALTER DATABASE [%s] SET ACCELERATED_DATABASE_RECOVERY = %s", name, boolToOnOff(*opts.AcceleratedDatabaseRecovery))
		tflog.Debug(ctx, fmt.Sprintf("Setting database option: %s", stmt))
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			errors = append(errors, fmt.Sprintf("ACCELERATED_DATABASE_RECOVERY: %v", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to set database options: %s", strings.Join(errors, "; "))
	}

	return nil
}

func boolToOnOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func (m client) GetDatabaseScopedConfigurations(ctx context.Context, name string) ([]DatabaseScopedConfiguration, error) {
	var configs []DatabaseScopedConfiguration

	conn, closeConn, err := m.getConnForDatabase(name)
	if err != nil {
		return configs, err
	}
	if closeConn {
		defer conn.Close()
	}

	cmd := `SELECT
		[name],
		CAST([value] AS NVARCHAR(MAX)) AS value,
		CAST([value_for_secondary] AS NVARCHAR(MAX)) AS value_for_secondary
	FROM sys.database_scoped_configurations
	WHERE [value] IS NOT NULL`

	tflog.Debug(ctx, fmt.Sprintf("Getting database scoped configurations for %s", name))
	rows, err := conn.QueryContext(ctx, cmd)
	if err != nil {
		return configs, err
	}
	defer rows.Close()

	for rows.Next() {
		var config DatabaseScopedConfiguration
		var valueForSecondary sql.NullString
		err := rows.Scan(&config.Name, &config.Value, &valueForSecondary)
		if err != nil {
			return configs, err
		}
		if valueForSecondary.Valid {
			config.ValueForSecondary = valueForSecondary.String
		}
		configs = append(configs, config)
	}

	return configs, rows.Err()
}

func (m client) SetDatabaseScopedConfiguration(ctx context.Context, name string, config DatabaseScopedConfiguration) error {
	conn, closeConn, err := m.getConnForDatabase(name)
	if err != nil {
		return err
	}
	if closeConn {
		defer conn.Close()
	}

	cmd := fmt.Sprintf("ALTER DATABASE SCOPED CONFIGURATION SET %s = %s", config.Name, config.Value)
	tflog.Debug(ctx, fmt.Sprintf("Setting database scoped configuration: %s", cmd))
	if _, err = conn.ExecContext(ctx, cmd); err != nil {
		return err
	}

	// Handle secondary only when a value is provided (skip on non-AG / single-instance)
	if config.ValueForSecondary != "" {
		cmd = fmt.Sprintf("ALTER DATABASE SCOPED CONFIGURATION FOR SECONDARY SET %s = %s", config.Name, config.ValueForSecondary)
		tflog.Debug(ctx, fmt.Sprintf("Setting database scoped configuration for secondary: %s", cmd))
		if _, err = conn.ExecContext(ctx, cmd); err != nil {
			return err
		}
	}

	return nil
}

func (m client) ClearDatabaseScopedConfiguration(ctx context.Context, name string, configName string) error {
	conn, closeConn, err := m.getConnForDatabase(name)
	if err != nil {
		return err
	}
	if closeConn {
		defer conn.Close()
	}

	cmd := fmt.Sprintf("ALTER DATABASE SCOPED CONFIGURATION CLEAR %s", configName)
	tflog.Debug(ctx, fmt.Sprintf("Clearing database scoped configuration: %s", cmd))
	_, err = conn.ExecContext(ctx, cmd)
	return err
}

// ExecScript executes an arbitrary SQL script in the specified database
func (m client) ExecScript(ctx context.Context, database string, script string) error {
	conn, closeConn, err := m.getConnForDatabase(database)
	if err != nil {
		return err
	}
	if closeConn {
		defer conn.Close()
	}

	batches := splitBatches(script)
	tflog.Debug(ctx, fmt.Sprintf("Executing script in database %s (%d batches, total %d chars)", database, len(batches), len(script)))

	for i, batch := range batches {
		batch = strings.TrimSpace(batch)
		if batch == "" {
			continue
		}
		tflog.Debug(ctx, fmt.Sprintf("Executing batch %d/%d", i+1, len(batches)))
		_, err := conn.ExecContext(ctx, batch)
		if err != nil {
			return fmt.Errorf("failed to execute batch %d: %v", i+1, err)
		}
	}

	return nil
}

// splitBatches splits a SQL script by GO batch separators
func splitBatches(script string) []string {
	// Split by GO on its own line (case-insensitive)
	// GO can have optional count like GO 5, but we'll just handle plain GO
	lines := strings.Split(script, "\n")
	var batches []string
	var currentBatch strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Check if line is just "GO" (case-insensitive)
		if strings.EqualFold(trimmed, "GO") || strings.HasPrefix(strings.ToUpper(trimmed), "GO ") {
			if currentBatch.Len() > 0 {
				batches = append(batches, currentBatch.String())
				currentBatch.Reset()
			}
		} else {
			currentBatch.WriteString(line)
			currentBatch.WriteString("\n")
		}
	}

	// Don't forget the last batch
	if currentBatch.Len() > 0 {
		batches = append(batches, currentBatch.String())
	}

	return batches
}
