package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	var migrationsPath string
	var databaseURL string

	flag.StringVar(&migrationsPath, "migrations-path", "", "path to migrations")
	flag.StringVar(&databaseURL, "database-url", os.Getenv("POSTGRES_URL"), "postgres database URL")
	flag.Parse()

	if err := migrateUp(migrationsPath, databaseURL); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func migrateUp(migrationsPath, databaseURL string) error {
	const op = "Migrator.MigrateUp"

	if migrationsPath == "" {
		return fmt.Errorf("%s: migrations-path is required", op)
	}
	if databaseURL == "" {
		return fmt.Errorf("%s: database-url or POSTGRES_URL is required", op)
	}

	m, err := migrate.New("file://"+migrationsPath, databaseURL)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			fmt.Println("no migrations to apply")
			return nil
		}

		return fmt.Errorf("%s: %w", op, err)
	}

	fmt.Println("migrations applied successfully")
	return nil
}
