package mysql

import (
	"fmt"
	"gofile/model"
	"log/slog"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

// Init 初始化 GORM 连接池 + AutoMigrate
func Init(dsn string) error {
	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		SkipDefaultTransaction: true,                 // 单表操作不自动包装事务
		PrepareStmt:            true,                  // 缓存预编译，提升性能
		Logger:                 logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return fmt.Errorf("open DB failed: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB failed: %w", err)
	}

	if err = sqlDB.Ping(); err != nil {
		return fmt.Errorf("ping DB failed: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// AutoMigrate: 自动建表/加列，不删除已有数据
	if err := db.AutoMigrate(
		&model.File{},
		&model.UserFile{},
		&model.User{},
		&model.Token{},
	); err != nil {
		return fmt.Errorf("auto migrate failed: %w", err)
	}

	slog.Info("MySQL connected and migrated")
	return nil
}

// DBConn 返回 GORM 数据库连接对象
func DBConn() *gorm.DB {
	return db
}