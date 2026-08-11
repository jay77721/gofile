package mysql

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migrateMysql "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

// Init 初始化 GORM 连接池 + 运行版本化迁移
func Init(dsn string) error {
	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		SkipDefaultTransaction: true, // 单表操作不自动包装事务
		PrepareStmt:            true, // 缓存预编译，提升性能
		Logger:                 logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return fmt.Errorf("open DB failed: %w", err)
	}

	gormDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB failed: %w", err)
	}

	if err = gormDB.Ping(); err != nil {
		return fmt.Errorf("ping DB failed: %w", err)
	}

	gormDB.SetMaxOpenConns(25)
	gormDB.SetMaxIdleConns(10)
	gormDB.SetConnMaxLifetime(5 * time.Minute)

	// 版本化迁移：使用 golang-migrate 按顺序执行 migrations/*.up.sql
	if err := runMigrations(gormDB); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	slog.Info("MySQL connected and migrated")
	return nil
}

// runMigrations 执行 migrations/ 目录下的 SQL 迁移文件
func runMigrations(db *sql.DB) error {
	driver, err := migrateMysql.WithInstance(db, &migrateMysql.Config{})
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

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration up failed: %w", err)
	}

	slog.Info("database migrations applied")
	return nil
}

// DBConn 返回 GORM 数据库连接对象
func DBConn() *gorm.DB {
	return db
}
