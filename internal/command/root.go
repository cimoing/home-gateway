package command

import (
	"context"
	"fmt"
	"time"

	"home-gateway/internal/database"

	"github.com/jmoiron/sqlx"
	"github.com/spf13/cobra"
)

// Execute runs the command-line application.
func Execute(ctx context.Context) error {
	return newRootCommand().ExecuteContext(ctx)
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "home-gateway",
		Short:         "Home Gateway service and administration CLI",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.AddCommand(newRunCommand(), newUserCommand())
	return root
}

func openDatabase(ctx context.Context) (*sqlx.DB, string, error) {
	config, err := database.ConfigFromEnv()
	if err != nil {
		return nil, "", fmt.Errorf("invalid database configuration: %w", err)
	}

	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	db, err := database.Open(connectCtx, config)
	if err != nil {
		return nil, "", fmt.Errorf("database connection failed: %w", err)
	}
	if err := database.Migrate(connectCtx, db, config.Driver); err != nil {
		db.Close()
		return nil, "", fmt.Errorf("database migration failed: %w", err)
	}
	return db, config.Driver, nil
}
