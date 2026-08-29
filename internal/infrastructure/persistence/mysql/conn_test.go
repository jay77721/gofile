package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
)

type testDriverFunc func(string) (driver.Conn, error)

func (f testDriverFunc) Open(name string) (driver.Conn, error) {
	return f(name)
}

type testDriverConn struct {
	mu      sync.Mutex
	pingErr error
	closed  bool
}

func (c *testDriverConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("test driver does not support statements")
}

func (c *testDriverConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *testDriverConn) Begin() (driver.Tx, error) {
	return nil, errors.New("test driver does not support transactions")
}

func (c *testDriverConn) Ping(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pingErr
}

func (c *testDriverConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

var testDriverID uint64

func newTestSQLDB(t *testing.T, pingErr error) (*sql.DB, *testDriverConn) {
	t.Helper()

	driverConn := &testDriverConn{pingErr: pingErr}
	driverName := fmt.Sprintf("gofile-test-%d", atomic.AddUint64(&testDriverID, 1))
	sql.Register(driverName, testDriverFunc(func(string) (driver.Conn, error) {
		return driverConn, nil
	}))

	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open test SQL DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, driverConn
}

func TestOpenConnectionClosesOnPingFailure(t *testing.T) {
	pingErr := errors.New("database is unavailable")
	sqlDB, driverConn := newTestSQLDB(t, pingErr)

	_, err := openConnection(
		"test-dsn",
		func(string) (*gorm.DB, error) { return &gorm.DB{}, nil },
		func(*gorm.DB) (*sql.DB, error) { return sqlDB, nil },
		func(string) error { t.Fatal("migration must not run after ping failure"); return nil },
	)
	if !errors.Is(err, pingErr) {
		t.Fatalf("error = %v, want ping error %v", err, pingErr)
	}
	if !driverConn.isClosed() {
		t.Fatal("expected SQL connection to close after ping failure")
	}
}

func TestOpenConnectionClosesOnMigrationFailure(t *testing.T) {
	migrationErr := errors.New("migration is unavailable")
	sqlDB, driverConn := newTestSQLDB(t, nil)

	conn, err := openConnection(
		"test-dsn",
		func(string) (*gorm.DB, error) { return &gorm.DB{}, nil },
		func(*gorm.DB) (*sql.DB, error) { return sqlDB, nil },
		func(string) error { return migrationErr },
	)
	if conn != nil {
		t.Fatal("expected no connection on migration failure")
	}
	if !errors.Is(err, migrationErr) {
		t.Fatalf("error = %v, want migration error %v", err, migrationErr)
	}
	if !driverConn.isClosed() {
		t.Fatal("expected SQL connection to close after migration failure")
	}
}

func TestConnectionCloseIsIdempotent(t *testing.T) {
	sqlDB, driverConn := newTestSQLDB(t, nil)
	gormDB := &gorm.DB{}
	conn := &Connection{gormDB: gormDB, sqlDB: sqlDB}
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("Ping test SQL DB: %v", err)
	}

	if conn.DB() != gormDB {
		t.Fatal("DB did not return the owned GORM handle")
	}
	if conn.SQLDB() != sqlDB {
		t.Fatal("SQLDB did not return the owned SQL pool")
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
	if !driverConn.isClosed() {
		t.Fatal("expected SQL connection to close")
	}
}

func TestConnectionNilAccessors(t *testing.T) {
	var conn *Connection
	if conn.DB() != nil {
		t.Fatal("nil connection DB should be nil")
	}
	if conn.SQLDB() != nil {
		t.Fatal("nil connection SQLDB should be nil")
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("nil connection Close failed: %v", err)
	}
}
