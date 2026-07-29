package intel

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
)

// fakeConnector yields a non-nil *sql.DB via sql.OpenDB without opening a real
// connection. NewSQLStore only nil-checks the handle, so this exercises the
// constructor's happy path without needing a database driver in CI.
type fakeConnector struct{}

func (fakeConnector) Connect(context.Context) (driver.Conn, error) { return nil, driver.ErrBadConn }
func (fakeConnector) Driver() driver.Driver                        { return nil }

func sqlDBStub() *sql.DB { return sql.OpenDB(fakeConnector{}) }

func TestSQLStore_NilDBRejected(t *testing.T) {
	if _, err := NewSQLStore(nil, "clickhouse"); err == nil {
		t.Fatalf("NewSQLStore(nil, ...) must return an error (fail fast on misconfig)")
	}
}

func TestSQLStore_ReportsRealBackend(t *testing.T) {
	s, err := NewSQLStore(sqlDBStub(), "clickhouse")
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	if !s.IsReal() {
		t.Fatalf("SQLStore.IsReal() must be true (real external backend)")
	}
	if s.Driver() != "clickhouse" {
		t.Fatalf("Driver()=%q, want clickhouse", s.Driver())
	}
	var _ Store = s // interface satisfaction (also asserted at package scope)
}

func TestSQLStore_DefaultDriverName(t *testing.T) {
	s, err := NewSQLStore(sqlDBStub(), "")
	if err != nil {
		t.Fatalf("NewSQLStore with empty driver should succeed: %v", err)
	}
	if s.Driver() != "sql" {
		t.Fatalf("empty driver should default to %q, got %q", "sql", s.Driver())
	}
}
