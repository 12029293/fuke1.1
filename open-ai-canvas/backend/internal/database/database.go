package database

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"yingce/backend/internal/logging"

	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Config struct {
	Driver  string
	DSN     string
	DataDir string
}

// sqlitePragmas 使用 glebarez/go-sqlite 真正识别的 `_pragma` 语法。
// 旧值 `_busy_timeout` / `_journal_mode` / `_foreign_keys` 不被该驱动解析（只有 `_pragma`
// 会被当成 "pragma <value>" 执行），因此 WAL、外键、同步级别长期没有生效：
// 库一直停在 journal_mode=delete，任何并发读写都退化成互斥，写侧直接返回
// `database is locked (SQLITE_BUSY)`，作品保存的 checkpoint 写不进去，任务被判失败。
// _txlock=immediate 让每个事务一开始就拿写锁（RESERVED），彻底避免 WAL 下最典型的
// `SQLITE_BUSY_SNAPSHOT`(517)：先 SELECT 建立读快照、再 UPDATE 想升级为写锁时，
// 若期间有其他连接提交，快照即失效，且 busy_timeout 对这类错误不重试，直接失败
// （日志表现为 duration_ms=0 + `database is locked (517)`）。
// busy_timeout 同时放宽到 15s，覆盖下载/落盘期间的长写事务。
const sqlitePragmas = "_pragma=busy_timeout(15000)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=foreign_keys(1)" +
	"&_pragma=synchronous(NORMAL)" +
	"&_txlock=immediate"

// enableSQLiteWAL 幂等地把库切到 WAL。journal_mode 持久化在库头里，
// 切换成功后后续连接即使不带参数也保持 WAL。只读挂载或内存库会返回原模式，
// 由 PRAGMA 自身处理，不阻断启动。
func enableSQLiteWAL(db *gorm.DB) error {
	return db.Exec("PRAGMA journal_mode=WAL").Error
}

func Open(config Config) (*gorm.DB, error) {
	driver := strings.ToLower(strings.TrimSpace(config.Driver))
	if driver == "" {
		driver = "sqlite"
	}
	switch driver {
	case "sqlite":
		dsn := strings.TrimSpace(config.DSN)
		if dsn == "" {
			if err := os.MkdirAll(config.DataDir, 0o755); err != nil {
				return nil, err
			}
			dsn = config.DataDir + "/open_ai_canvas.db?" + sqlitePragmas
		}
		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logging.GormLogger()})
		if err != nil {
			return nil, err
		}
		// DSN 里的 _pragma 只在新建连接时生效；WAL 是库级持久设置，这里再确保一次，
		// 让已经用旧 DSN（journal_mode=delete）打开过的库也能切到 WAL，
		// 避免写事务与读事务互斥导致 checkpoint 写入直接 SQLITE_BUSY。
		if err := enableSQLiteWAL(db); err != nil {
			return nil, err
		}
		return db, nil
	case "postgres", "postgresql":
		dsn := strings.TrimSpace(config.DSN)
		if dsn == "" {
			return nil, errors.New("PostgreSQL 模式必须配置 DATABASE_URL")
		}
		return gorm.Open(postgres.New(postgres.Config{
			DSN:          dsn,
			OptionOpenDB: []stdlib.OptionOpenDB{stdlib.OptionBeforeConnect(configurePostgresConnection)},
		}), &gorm.Config{Logger: logging.GormLogger()})
	default:
		return nil, fmt.Errorf("不支持的数据库驱动：%s", driver)
	}
}

func ConfigurePool(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	if db.Dialector.Name() == "postgres" {
		sqlDB.SetMaxOpenConns(30)
		sqlDB.SetMaxIdleConns(10)
		return nil
	}
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(4)
	return nil
}
