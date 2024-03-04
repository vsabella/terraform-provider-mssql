package mssql

type MssqlClient struct {
}

func (*MssqlClient) DoStuff() string {
	return "Hi Mom!"
}

func (*MssqlClient) IsConnectionOK() bool {
	return true
}
