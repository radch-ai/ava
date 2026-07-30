package testhelpers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func projectRoot() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "schema/migrations"
	}
	return filepath.Join(filepath.Dir(filename), "..", "..")
}

type TestDB struct {
	Pool    *pgxpool.Pool
	ConnStr string
	dbName  string
}

var (
	sharedContainer     *postgres.PostgresContainer
	sharedContainerOnce sync.Once
	sharedContainerErr  error
	sharedConnStr       string
)

func getSharedContainer(ctx context.Context) (*postgres.PostgresContainer, string, error) {
	sharedContainerOnce.Do(func() {
		sharedContainer, sharedContainerErr = postgres.Run(ctx, "postgres:18-alpine",
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(60*time.Second),
			),
		)
		if sharedContainerErr != nil {
			return
		}
		sharedConnStr, sharedContainerErr = sharedContainer.ConnectionString(ctx, "sslmode=disable")
	})
	return sharedContainer, sharedConnStr, sharedContainerErr
}

func SetupTestDB(ctx context.Context, t *testing.T) *TestDB {
	t.Helper()

	_, baseConnStr, err := getSharedContainer(ctx)
	if err != nil {
		t.Fatalf("failed to start shared container: %v", err)
	}

	dbName := fmt.Sprintf("testdb_%d", time.Now().UnixNano())

	conn, err := pgx.Connect(ctx, baseConnStr)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
	if err != nil {
		t.Fatalf("failed to create database %s: %v", dbName, err)
	}

	testConnStr := fmt.Sprintf("%s&dbname=%s", baseConnStr, dbName)

	poolConfig, err := pgxpool.ParseConfig(testConnStr)
	if err != nil {
		t.Fatalf("failed to parse connection string: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}

	testDB := &TestDB{
		Pool:    pool,
		ConnStr: testConnStr,
		dbName:  dbName,
	}

	if err := testDB.runMigrations(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	t.Cleanup(func() {
		go pool.Close()
	})

	return testDB
}

func (tdb *TestDB) runMigrations(ctx context.Context) error {
	entries, err := filepath.Glob(filepath.Join(projectRoot(), "schema", "migrations", "*.up.sql"))
	if err != nil {
		return fmt.Errorf("failed to list migrations: %w", err)
	}

	sort.Strings(entries)

	for _, entry := range entries {
		sql, err := os.ReadFile(entry)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", entry, err)
		}

		if _, err := tdb.Pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", entry, err)
		}
	}

	return nil
}

func (tdb *TestDB) CleanupTable(t *testing.T, table string) {
	t.Helper()
	ctx := context.Background()
	_, err := tdb.Pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s", table))
	if err != nil {
		t.Fatalf("failed to cleanup table %s: %v", table, err)
	}
}
