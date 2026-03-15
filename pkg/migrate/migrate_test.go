package migrate

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestChecksum(t *testing.T) {
	sql := "CREATE TABLE users (id UUID PRIMARY KEY);"
	got := Checksum(sql)
	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(sql)))
	if got != expected {
		t.Errorf("Checksum = %q, want %q", got, expected)
	}
}

func TestChecksum_DifferentSQL(t *testing.T) {
	c1 := Checksum("CREATE TABLE a (id INT);")
	c2 := Checksum("CREATE TABLE b (id INT);")
	if c1 == c2 {
		t.Error("different SQL should produce different checksums")
	}
}

func TestChecksum_Deterministic(t *testing.T) {
	sql := "SELECT 1"
	c1 := Checksum(sql)
	c2 := Checksum(sql)
	if c1 != c2 {
		t.Error("same SQL should produce same checksum")
	}
}

func TestBuiltinMigrations_Count(t *testing.T) {
	migrations := BuiltinMigrations()
	if len(migrations) < 7 {
		t.Errorf("expected at least 7 builtin migrations, got %d", len(migrations))
	}
}

func TestBuiltinMigrations_Ordered(t *testing.T) {
	migrations := BuiltinMigrations()
	for i := 1; i < len(migrations); i++ {
		if migrations[i].Version <= migrations[i-1].Version {
			t.Errorf("migrations not in order: %s should be after %s",
				migrations[i].Version, migrations[i-1].Version)
		}
	}
}

func TestBuiltinMigrations_HaveUpAndDown(t *testing.T) {
	for _, m := range BuiltinMigrations() {
		if m.UpSQL == "" {
			t.Errorf("migration %s: UpSQL is empty", m.Version)
		}
		if m.DownSQL == "" {
			t.Errorf("migration %s: DownSQL is empty", m.Version)
		}
		if m.Description == "" {
			t.Errorf("migration %s: Description is empty", m.Version)
		}
	}
}

func TestBuiltinMigrations_UniqueVersions(t *testing.T) {
	seen := make(map[string]bool)
	for _, m := range BuiltinMigrations() {
		if seen[m.Version] {
			t.Errorf("duplicate migration version: %s", m.Version)
		}
		seen[m.Version] = true
	}
}

func TestNew_DefaultTableName(t *testing.T) {
	m := New(Config{}, nil)
	if m.tableName != "schema_migrations" {
		t.Errorf("default tableName = %q, want 'schema_migrations'", m.tableName)
	}
}

func TestNew_CustomTableName(t *testing.T) {
	m := New(Config{TableName: "my_migrations"}, nil)
	if m.tableName != "my_migrations" {
		t.Errorf("tableName = %q, want 'my_migrations'", m.tableName)
	}
}

func TestNew_SortsMigrations(t *testing.T) {
	// Provide out-of-order migrations
	migrations := []Migration{
		{Version: "0003", UpSQL: "C"},
		{Version: "0001", UpSQL: "A"},
		{Version: "0002", UpSQL: "B"},
	}
	m := New(Config{}, migrations)
	for i := 1; i < len(m.migrations); i++ {
		if m.migrations[i].Version <= m.migrations[i-1].Version {
			t.Errorf("migrations not sorted: %s after %s", m.migrations[i].Version, m.migrations[i-1].Version)
		}
	}
}

func TestDirection_Constants(t *testing.T) {
	if Up != 0 {
		t.Errorf("Up = %d, want 0", Up)
	}
	if Down != 1 {
		t.Errorf("Down = %d, want 1", Down)
	}
}
