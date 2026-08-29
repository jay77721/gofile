package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migrateMysql "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connection owns both the GORM handle and its underlying SQL pool.
// Call Close when the application is shutting down.
type Connection struct {
	gormDB *gorm.DB
	sqlDB  *sql.DB

	closeOnce sync.Once
	closeErr  error
}

// DB returns the GORM database handle owned by the connection.
func (c *Connection) DB() *gorm.DB {
	if c == nil {
		return nil
	}
	return c.gormDB
}

// SQLDB returns the underlying database/sql pool for lifecycle or pool
// statistics. Callers must not close it directly; use Connection.Close.
func (c *Connection) SQLDB() *sql.DB {
	if c == nil {
		return nil
	}
	return c.sqlDB
}

// Close releases the underlying SQL pool. It is safe to call more than once.
func (c *Connection) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.sqlDB != nil {
			c.closeErr = c.sqlDB.Close()
		}
	})
	return c.closeErr
}

type gormOpener func(dsn string) (*gorm.DB, error)
type sqlDBGetter func(db *gorm.DB) (*sql.DB, error)
type migrationRunner func(dsn string) error

// Open creates, pings, configures, and migrates a MySQL connection.
// The returned Connection owns the resources and must be closed by its owner.
func Open(dsn string) (*Connection, error) {
	return openConnection(
		dsn,
		func(dsn string) (*gorm.DB, error) {
			return gorm.Open(mysql.Open(dsn), &gorm.Config{
				SkipDefaultTransaction: true, // Single-table operations are not automatically wrapped in a transaction
				PrepareStmt:            true, // Cache prepared statements for better performance
				Logger:                 logger.Default.LogMode(logger.Warn),
			})
		},
		func(db *gorm.DB) (*sql.DB, error) {
			return db.DB()
		},
		runMigrations,
	)
}

// Connect is an explicit alias for Open for callers that prefer connection
// terminology.
func Connect(dsn string) (*Connection, error) {
	return Open(dsn)
}

func openConnection(dsn string, openGORM gormOpener, getSQLDB sqlDBGetter, migrate migrationRunner) (*Connection, error) {
	gormDB, err := openGORM(dsn)
	if err != nil {
		return nil, fmt.Errorf("open DB failed: %w", err)
	}

	sqlDB, err := getSQLDB(gormDB)
	if err != nil {
		return nil, fmt.Errorf("get sql.DB failed: %w", err)
	}
	conn := &Connection{gormDB: gormDB, sqlDB: sqlDB}

	if err := sqlDB.Ping(); err != nil {
		return closeFailedConnection(conn, fmt.Errorf("ping DB failed: %w", err))
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	if err := migrate(dsn); err != nil {
		return closeFailedConnection(conn, fmt.Errorf("migration failed: %w", err))
	}

	slog.Info("MySQL connected and migrated")
	return conn, nil
}

func closeFailedConnection(conn *Connection, cause error) (*Connection, error) {
	if err := conn.Close(); err != nil {
		return nil, errors.Join(cause, fmt.Errorf("close DB after initialization failure: %w", err))
	}
	return nil, cause
}

// runMigrations executes migrations/ using a dedicated SQL pool. The migration
// driver closes that pool before returning so it cannot retain a connection
// leased from the application's main pool.
func runMigrations(dsn string) (err error) {
	migrationDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open migration DB failed: %w", err)
	}
	defer func() {
		if closeErr := migrationDB.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close migration DB failed: %w", closeErr))
		}
	}()

	driver, err := migrateMysql.WithInstance(migrationDB, &migrateMysql.Config{})
	if err != nil {
		return fmt.Errorf("create migrate driver failed: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://migrations",
		"mysql",
		driver,
	)
	if err != nil {
		return fmt.Errorf("create migrate instance failed: %w", err)
	}
	defer func() {
		sourceErr, databaseErr := m.Close()
		if sourceErr != nil {
			err = errors.Join(err, fmt.Errorf("close migration source failed: %w", sourceErr))
		}
		if databaseErr != nil {
			err = errors.Join(err, fmt.Errorf("close migration driver failed: %w", databaseErr))
		}
	}()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration up failed: %w", err)
	}

	slog.Info("database migrations applied")
	return nil
}
