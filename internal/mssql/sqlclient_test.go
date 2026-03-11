package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func Test_buildCreateUser(t *testing.T) {
	type args struct {
		create CreateUser
	}
	tests := []struct {
		name  string
		args  args
		want  string
		want1 []any
		want2 error
	}{
		{
			name: "User with Password",
			args: args{CreateUser{
				Username:      "user",
				Password:      "password",
				Sid:           "",
				External:      false,
				DefaultSchema: "dbo",
			}},
			want:  `DECLARE @sql NVARCHAR(max);SET @sql = 'CREATE USER ' + QUOTENAME(@username) + 'WITH ' + 'DEFAULT_SCHEMA = ' + QUOTENAME(@default_schema) + ', ' + 'PASSWORD = ' + QUOTENAME(@password,'''');EXEC (@sql);`,
			want1: []any{sql.Named("username", "user"), sql.Named("default_schema", "dbo"), sql.Named("password", "password")},
		},
		{
			name: "User with Password and SID",
			args: args{CreateUser{
				Username:      "user",
				Password:      "password",
				Sid:           "SOMESID",
				External:      false,
				DefaultSchema: "dbo",
			}},
			want:  `DECLARE @sql NVARCHAR(max);SET @sql = 'CREATE USER ' + QUOTENAME(@username) + 'WITH ' + 'DEFAULT_SCHEMA = ' + QUOTENAME(@default_schema) + ', ' + 'PASSWORD = ' + QUOTENAME(@password,'''') + ', ' + 'SID = ' + QUOTENAME(@sid,'''');EXEC (@sql);`,
			want1: []any{sql.Named("username", "user"), sql.Named("default_schema", "dbo"), sql.Named("password", "password"), sql.Named("sid", "SOMESID")},
		},
		{
			name: "User with Login",
			args: args{CreateUser{
				Username:      "app_user",
				LoginName:     "app_login",
				Password:      "",
				Sid:           "",
				External:      false,
				DefaultSchema: "dbo",
			}},
			want:  `DECLARE @sql NVARCHAR(max);SET @sql = 'CREATE USER ' + QUOTENAME(@username) + ' FOR LOGIN ' + QUOTENAME(@login_name) + 'WITH ' + 'DEFAULT_SCHEMA = ' + QUOTENAME(@default_schema);EXEC (@sql);`,
			want1: []any{sql.Named("username", "app_user"), sql.Named("login_name", "app_login"), sql.Named("default_schema", "dbo")},
		},
		{
			name: "External User",
			args: args{CreateUser{
				Username:      "bob@contoso.com",
				Password:      "",
				Sid:           "",
				External:      true,
				DefaultSchema: "dbo",
			}},
			want:  `DECLARE @sql NVARCHAR(max);SET @sql = 'CREATE USER ' + QUOTENAME(@username) + ' FROM EXTERNAL PROVIDER ' + 'WITH ' + 'DEFAULT_SCHEMA = ' + QUOTENAME(@default_schema);EXEC (@sql);`,
			want1: []any{sql.Named("username", "bob@contoso.com"), sql.Named("default_schema", "dbo")},
		},
		{
			name: "Error No Default Schema",
			args: args{CreateUser{
				Username:      "user",
				Password:      "password",
				Sid:           "SOMESID",
				External:      false,
				DefaultSchema: "",
			}},
			want2: errors.New("invalid user user, default schema must be specified"),
		},
		{
			name: "Error External and Password",
			args: args{CreateUser{
				Username:      "user",
				Password:      "password",
				Sid:           "",
				External:      true,
				DefaultSchema: "",
			}},
			want2: errors.New("invalid user user, external users may not have passwords"),
		},
		{
			name: "Error External and SID",
			args: args{CreateUser{
				Username:      "user",
				Password:      "",
				Sid:           "SID",
				External:      true,
				DefaultSchema: "",
			}},
			want2: errors.New("invalid user user, external users must not have a SID"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := buildCreateUser(tt.args.create)
			got = strings.ReplaceAll(got, "\n", "")
			tt.want = strings.ReplaceAll(tt.want, "\n", "")

			if (err != nil && tt.want2 != nil) && err.Error() != tt.want2.Error() {
				t.Errorf("buildCreateUser() err = %v, want2 %v", err, tt.want2)
			}
			if got != tt.want {
				t.Errorf("buildCreateUser() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("buildCreateUser() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func Test_CreateDatabase(t *testing.T) {
	password := os.Getenv("MSSQL_SA_PASSWORD")
	if password == "" {
		t.Fatalf("MSSQL_SA_PASSWORD environment variable is not set")
	}

	// Use 127.0.0.1 instead of localhost to avoid IPv6 ::1 resolution issues on some systems.
	c, ok := NewClient("127.0.0.1", 1433, "master", "sa", password).(*client)
	if !ok {
		t.Fatalf("expected *client from NewClient")
	}
	ctx := context.Background()

	t.Run("Create valid database", func(t *testing.T) {
		name := fmt.Sprintf("testdb_%d", time.Now().UnixNano())
		db, err := c.CreateDatabase(ctx, name)
		if err != nil {
			t.Fatalf("CreateDatabase() error = %v", err)
		}
		defer func() {
			if _, err := c.conn.ExecContext(ctx, fmt.Sprintf("DROP DATABASE [%s]", db.Name)); err != nil {
				t.Logf("failed to drop database %s: %v", db.Name, err)
			}
		}()
	})

	t.Run("Create existing database", func(t *testing.T) {
		name := fmt.Sprintf("testdb_%d", time.Now().UnixNano())
		db, err := c.CreateDatabase(ctx, name)
		if err != nil {
			t.Fatalf("setup create failed: %v", err)
		}
		defer func() {
			if _, err := c.conn.ExecContext(ctx, fmt.Sprintf("DROP DATABASE [%s]", db.Name)); err != nil {
				t.Logf("failed to drop database %s: %v", db.Name, err)
			}
		}()

		if _, err := c.CreateDatabase(ctx, name); err == nil {
			t.Fatalf("expected error creating existing database, got nil")
		}
	})

	t.Run("Create database with invalid name", func(t *testing.T) {
		if _, err := c.CreateDatabase(ctx, ""); err == nil {
			t.Fatalf("expected error for empty name")
		}
	})
}

func Test_SetDatabaseOptions_NoChanges(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() err = %v", err)
	}
	defer db.Close()

	c := client{conn: db}
	opts := DatabaseOptions{}

	if err := c.SetDatabaseOptions(context.Background(), "testdb", opts); err != nil {
		t.Fatalf("SetDatabaseOptions() unexpected err = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func Test_SetDatabaseOptions_OnlyRCSI(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() err = %v", err)
	}
	defer db.Close()

	c := client{conn: db}
	rcsi := true
	opts := DatabaseOptions{
		ReadCommittedSnapshot: &rcsi,
	}

	mock.ExpectExec("ALTER DATABASE").
		WithArgs(sql.Named("db", "testdb")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := c.SetDatabaseOptions(context.Background(), "testdb", opts); err != nil {
		t.Fatalf("SetDatabaseOptions() unexpected err = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func Test_validateIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		wantErr bool
	}{
		{name: "valid basic", val: "user-test_sql@1", wantErr: false},
		{name: "valid domain", val: "DOMAIN\\user", wantErr: false},
		{name: "valid space", val: "NT AUTHORITY\\SYSTEM", wantErr: false},
		{name: "valid dot", val: "schema.object", wantErr: false},
		{name: "empty", val: "", wantErr: true},
		{name: "invalid char", val: "bad]", wantErr: true},
		{name: "too long", val: strings.Repeat("a", 129), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIdentifier("field", tt.val)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateIdentifier() err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func Test_validatePermission(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		wantErr bool
	}{
		{name: "valid single", val: "SELECT", wantErr: false},
		{name: "valid multi word", val: "ALTER ANY LOGIN", wantErr: false},
		{name: "invalid char", val: "DROP; SELECT", wantErr: true},
		{name: "too long", val: strings.Repeat("A", 129), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePermission("perm", tt.val)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePermission() err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func Test_validateLoginSid(t *testing.T) {
	tests := []struct {
		name    string
		sid     string
		wantErr bool
	}{
		{name: "empty ok", sid: "", wantErr: false},
		{name: "valid sid", sid: "0x010500000000000515000000", wantErr: false},
		{name: "valid uppercase sid", sid: "0xD08B09D22C942847A8562F9A6178854E", wantErr: false},
		{name: "missing 0x prefix", sid: "0105000000", wantErr: true},
		{name: "odd length", sid: "0x123", wantErr: true},
		{name: "invalid chars", sid: "0x12ZZ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLoginSid(tt.sid)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for sid %q", tt.sid)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for sid %q: %v", tt.sid, err)
			}
		})
	}
}

func Test_CreateLogin_WithSid(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{conn: db}
	create := CreateLogin{
		Name:            "test_login",
		Password:        "Password123!",
		DefaultDatabase: "master",
		// Use mixed-case hex to ensure SID casing is normalized by the client/provider.
		Sid: "0xD08B09D22C942847A8562F9A6178854E",
	}
	wantSid := strings.ToLower(create.Sid)

	mock.ExpectExec("CREATE LOGIN").
		WithArgs(
			sql.Named("name", create.Name),
			sql.Named("password", create.Password),
			sql.Named("default_database", create.DefaultDatabase),
			sql.Named("sid", wantSid),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	rows := sqlmock.NewRows([]string{"name", "default_database", "default_language", "is_disabled", "sid"}).
		// Simulate SQL Server returning the SID in the original (mixed/upper) casing.
		AddRow(create.Name, "master", "", false, create.Sid)
	mock.ExpectQuery("FROM sys.server_principals").
		WithArgs(sql.Named("name", create.Name)).
		WillReturnRows(rows)

	login, err := c.CreateLogin(context.Background(), create)
	if err != nil {
		t.Fatalf("CreateLogin() error = %v", err)
	}
	if login.Sid != wantSid {
		t.Fatalf("CreateLogin() sid = %s, want %s", login.Sid, wantSid)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func Test_GetUser_LoginNameFallbackFromSqlLogins(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{
		conn:     db,
		database: "permdb1",
	}

	rows := sqlmock.NewRows([]string{"id", "sid", "name", "type", "ext", "default_schema_name", "sid_bytes"}).
		AddRow("tools-user", "0x01", "tools-user", "S", false, "tools", []byte{0x01})

	mock.ExpectQuery("FROM sys\\.database_principals").
		WithArgs(sql.Named("username", "tools-user")).
		WillReturnRows(rows)

	mock.ExpectQuery("FROM sys\\.server_principals").
		WithArgs(sql.Named("sid", []byte{0x01})).
		WillReturnRows(sqlmock.NewRows([]string{"name"}))

	mock.ExpectQuery("FROM sys\\.sql_logins").
		WithArgs(sql.Named("sid", []byte{0x01})).
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("tools-user"))

	user, err := c.GetUser(context.Background(), "permdb1", "tools-user")
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if user.LoginName != "tools-user" {
		t.Fatalf("GetUser() login_name = %q, want %q", user.LoginName, "tools-user")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func Test_GetUser_LoginNameFromServerPrincipals(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{
		conn:     db,
		database: "appdb",
	}

	rows := sqlmock.NewRows([]string{"id", "sid", "name", "type", "ext", "default_schema_name", "sid_bytes"}).
		AddRow("app-user", "0x02", "app-user", "S", false, "dbo", []byte{0x02})

	mock.ExpectQuery("FROM sys\\.database_principals").
		WithArgs(sql.Named("username", "app-user")).
		WillReturnRows(rows)

	mock.ExpectQuery("FROM sys\\.server_principals").
		WithArgs(sql.Named("sid", []byte{0x02})).
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("app-login"))

	user, err := c.GetUser(context.Background(), "appdb", "app-user")
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if user.LoginName != "app-login" {
		t.Fatalf("GetUser() login_name = %q, want %q", user.LoginName, "app-login")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func Test_GetUser_MissingSqlLoginsView_NoError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{
		conn:     db,
		database: "permdb1",
	}

	rows := sqlmock.NewRows([]string{"id", "sid", "name", "type", "ext", "default_schema_name", "sid_bytes"}).
		AddRow("tools-user", "0x01", "tools-user", "S", false, "tools", []byte{0x01})

	mock.ExpectQuery("FROM sys\\.database_principals").
		WithArgs(sql.Named("username", "tools-user")).
		WillReturnRows(rows)

	mock.ExpectQuery("FROM sys\\.server_principals").
		WithArgs(sql.Named("sid", []byte{0x01})).
		WillReturnRows(sqlmock.NewRows([]string{"name"}))

	mock.ExpectQuery("FROM sys\\.sql_logins").
		WithArgs(sql.Named("sid", []byte{0x01})).
		WillReturnError(fmt.Errorf("mssql: Invalid object name 'sys.sql_logins'."))

	user, err := c.GetUser(context.Background(), "permdb1", "tools-user")
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if user.LoginName != "" {
		t.Fatalf("GetUser() login_name = %q, want empty string", user.LoginName)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func Test_CreateLogin_MasterDefaultDatabaseFallback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{conn: db}
	create := CreateLogin{
		Name:            "test_login",
		Password:        "Password123!",
		DefaultDatabase: "master",
	}

	mock.ExpectExec("CREATE LOGIN").
		WithArgs(
			sql.Named("name", create.Name),
			sql.Named("password", create.Password),
			sql.Named("default_database", create.DefaultDatabase),
		).
		WillReturnError(fmt.Errorf("mssql: Keyword or statement option 'default_database' is not supported in this version of SQL Server."))

	mock.ExpectExec("CREATE LOGIN").
		WithArgs(
			sql.Named("name", create.Name),
			sql.Named("password", create.Password),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	rows := sqlmock.NewRows([]string{"name", "default_database", "default_language", "is_disabled", "sid"}).
		AddRow(create.Name, "master", "", false, "")
	mock.ExpectQuery("FROM sys.server_principals").
		WithArgs(sql.Named("name", create.Name)).
		WillReturnRows(rows)

	login, err := c.CreateLogin(context.Background(), create)
	if err != nil {
		t.Fatalf("CreateLogin() error = %v", err)
	}
	if login.Name != create.Name {
		t.Fatalf("CreateLogin() name = %s, want %s", login.Name, create.Name)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func Test_UpdateLogin_DefaultDatabaseMaster(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{conn: db}
	update := UpdateLogin{
		Name:            "test_login",
		DefaultDatabase: "master",
	}

	mock.ExpectExec("ALTER LOGIN").
		WithArgs(
			sql.Named("name", update.Name),
			sql.Named("default_database", update.DefaultDatabase),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	rows := sqlmock.NewRows([]string{"name", "default_database", "default_language", "is_disabled", "sid"}).
		AddRow(update.Name, "master", "", false, "")
	mock.ExpectQuery("FROM sys.server_principals").
		WithArgs(sql.Named("name", update.Name)).
		WillReturnRows(rows)

	login, err := c.UpdateLogin(context.Background(), update)
	if err != nil {
		t.Fatalf("UpdateLogin() error = %v", err)
	}
	if login.DefaultDatabase != "master" {
		t.Fatalf("UpdateLogin() default_database = %s, want master", login.DefaultDatabase)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func Test_UpdateLogin_DefaultDatabaseMasterFallback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{conn: db}
	update := UpdateLogin{
		Name:            "test_login",
		DefaultDatabase: "master",
	}

	mock.ExpectExec("ALTER LOGIN").
		WithArgs(
			sql.Named("name", update.Name),
			sql.Named("default_database", update.DefaultDatabase),
		).
		WillReturnError(fmt.Errorf("mssql: Keyword or statement option 'default_database' is not supported in this version of SQL Server."))

	rows := sqlmock.NewRows([]string{"name", "default_database", "default_language", "is_disabled", "sid"}).
		AddRow(update.Name, "master", "", false, "")
	mock.ExpectQuery("FROM sys.server_principals").
		WithArgs(sql.Named("name", update.Name)).
		WillReturnRows(rows)

	login, err := c.UpdateLogin(context.Background(), update)
	if err != nil {
		t.Fatalf("UpdateLogin() error = %v", err)
	}
	if login.DefaultDatabase != "master" {
		t.Fatalf("UpdateLogin() default_database = %s, want master", login.DefaultDatabase)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func Test_DeleteRole_DatabaseMissing_NoOp(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{conn: db, database: "master", connByDatabase: map[string]*sql.DB{}}

	mock.ExpectQuery("SELECT 1 FROM sys\\.databases WHERE \\[name\\] = @name").
		WithArgs(sql.Named("name", "missingdb")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}))

	if err := c.DeleteRole(context.Background(), "missingdb", "db_executor"); err != nil {
		t.Fatalf("DeleteRole() unexpected error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func Test_DeleteUser_DatabaseMissing_NoOp(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{conn: db, database: "master", connByDatabase: map[string]*sql.DB{}}

	mock.ExpectQuery("SELECT 1 FROM sys\\.databases WHERE \\[name\\] = @name").
		WithArgs(sql.Named("name", "missingdb")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}))

	if err := c.DeleteUser(context.Background(), "missingdb", "app_user"); err != nil {
		t.Fatalf("DeleteUser() unexpected error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func Test_RevokePermission_DatabaseMissing_NoOp(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{conn: db, database: "master", connByDatabase: map[string]*sql.DB{}}

	mock.ExpectQuery("SELECT 1 FROM sys\\.databases WHERE \\[name\\] = @name").
		WithArgs(sql.Named("name", "missingdb")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}))

	if err := c.RevokePermission(context.Background(), GrantPermission{
		Database:   "missingdb",
		Principal:  "db_executor",
		Permission: "EXECUTE",
	}); err != nil {
		t.Fatalf("RevokePermission() unexpected error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func Test_RevokePermission_DatabasePermission_ConditionalRevoke(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{conn: db, database: "permdb1", connByDatabase: map[string]*sql.DB{"permdb1": db}}

	mock.ExpectQuery("SELECT 1 FROM sys\\.databases WHERE \\[name\\] = @name").
		WithArgs(sql.Named("name", "permdb1")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))

	mock.ExpectExec("IF EXISTS").
		WithArgs(
			sql.Named("permission", "EXECUTE"),
			sql.Named("principal", "db_executor"),
		).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := c.RevokePermission(context.Background(), GrantPermission{
		Database:   "permdb1",
		Principal:  "db_executor",
		Permission: "execute",
	}); err != nil {
		t.Fatalf("RevokePermission() unexpected error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func Test_UnassignServerRole_ConditionalNoOp(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	c := &client{conn: db, database: "master", connByDatabase: map[string]*sql.DB{"master": db}}

	mock.ExpectExec("IF EXISTS").
		WithArgs(
			sql.Named("role", "##MS_ServerStateReader##"),
			sql.Named("principal", "telemetry-user"),
		).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := c.UnassignServerRole(context.Background(), "##MS_ServerStateReader##", "telemetry-user"); err != nil {
		t.Fatalf("UnassignServerRole() unexpected error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
