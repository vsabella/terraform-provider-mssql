package mssql

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
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
				Login:         "",
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
				Login:         "",
				DefaultSchema: "dbo",
			}},
			want:  `DECLARE @sql NVARCHAR(max);SET @sql = 'CREATE USER ' + QUOTENAME(@username) + 'WITH ' + 'DEFAULT_SCHEMA = ' + QUOTENAME(@default_schema) + ', ' + 'PASSWORD = ' + QUOTENAME(@password,'''') + ', ' + 'SID = ' + QUOTENAME(@sid,'''');EXEC (@sql);`,
			want1: []any{sql.Named("username", "user"), sql.Named("default_schema", "dbo"), sql.Named("password", "password"), sql.Named("sid", "SOMESID")},
		},
		{
			name: "User with Login",
			args: args{CreateUser{
				Username:      "user",
				Password:      "",
				Sid:           "",
				External:      false,
				Login:         "LOGIN",
				DefaultSchema: "dbo",
			}},
			want:  `DECLARE @sql NVARCHAR(max);SET @sql = 'CREATE USER ' + QUOTENAME(@username) + ' FROM LOGIN ' + QUOTENAME(@login) + 'WITH ' + 'DEFAULT_SCHEMA = ' + QUOTENAME(@default_schema);EXEC (@sql);`,
			want1: []any{sql.Named("username", "user"), sql.Named("login", "LOGIN"), sql.Named("default_schema", "dbo")},
		},
		{
			name: "External User",
			args: args{CreateUser{
				Username:      "bob@contoso.com",
				Password:      "",
				Sid:           "",
				External:      true,
				Login:         "",
				DefaultSchema: "dbo",
			}},
			want:  `DECLARE @sql NVARCHAR(max);SET @sql = 'CREATE USER ' + QUOTENAME(@username) + ' FROM EXTERNAL PROVIDER' + 'WITH ' + 'DEFAULT_SCHEMA = ' + QUOTENAME(@default_schema);EXEC (@sql);`,
			want1: []any{sql.Named("username", "bob@contoso.com"), sql.Named("default_schema", "dbo")},
		},
		{
			name: "Error No Default Schema",
			args: args{CreateUser{
				Username:      "user",
				Password:      "password",
				Sid:           "SOMESID",
				External:      false,
				Login:         "",
				DefaultSchema: "",
			}},
			want2: errors.New("invalid user user, default schema must be specified"),
		},
		{
			name: "Error Login and Password",
			args: args{CreateUser{
				Username:      "user",
				Password:      "password",
				Sid:           "",
				External:      false,
				Login:         "LOGIN",
				DefaultSchema: "",
			}},
			want2: errors.New("invalid user user, login users may not have passwords"),
		},
		{
			name: "Error External and Password",
			args: args{CreateUser{
				Username:      "user",
				Password:      "password",
				Sid:           "",
				External:      true,
				Login:         "",
				DefaultSchema: "",
			}},
			want2: errors.New("invalid user user, external users may not have passwords"),
		},
		{
			name: "Error External and Login",
			args: args{CreateUser{
				Username:      "user",
				Password:      "",
				Sid:           "",
				External:      true,
				Login:         "Login",
				DefaultSchema: "",
			}},
			want2: errors.New("invalid user user, external users must not have a login"),
		},
		{
			name: "Error External and SID",
			args: args{CreateUser{
				Username:      "user",
				Password:      "",
				Sid:           "SID",
				External:      true,
				Login:         "",
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
