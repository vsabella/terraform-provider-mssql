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
