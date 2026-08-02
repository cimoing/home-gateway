package command

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"home-gateway/internal/bt"
	appconfig "home-gateway/internal/config"
	"home-gateway/internal/credential"
	"home-gateway/internal/router"
	"home-gateway/internal/storage"

	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "run",
		Short: "Run the API service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServer(cmd)
		},
	}
	command.Flags().String("config", appconfig.DefaultPath, "path to YAML configuration")
	return command
}

func runServer(cmd *cobra.Command) error {
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}
	config, err := appconfig.Load(configPath, cmd.Flags().Changed("config"))
	if err != nil {
		return fmt.Errorf("invalid application configuration: %w", err)
	}

	db, driver, err := openDatabase(cmd.Context())
	if err != nil {
		return err
	}
	defer db.Close()
	log.Printf("database ready using %s", driver)

	var engine bt.Engine
	if config.BT.Enabled {
		if err := ensureWritableDirectory(config.BT.DownloadDir); err != nil {
			return err
		}
		anacrolix, err := bt.NewAnacrolixEngine(
			config.BT.DownloadDir,
			config.BT.ListenPort,
			config.BT.DownloadLimitBps,
			config.BT.UploadLimitBps,
		)
		if err != nil {
			return err
		}
		engine = anacrolix
	}
	storageService := storage.NewService(db, credential.FromEnv())
	if err := storageService.EnsureDefaultLocalBackend(cmd.Context(), config.BT.DownloadDir); err != nil {
		return fmt.Errorf("seed default storage backend: %w", err)
	}
	btService := bt.NewServiceWithStorage(db, engine, storageService, config.BT, configPath)
	defer func() {
		if err := btService.Close(); err != nil {
			log.Printf("BitTorrent shutdown failed: %v", err)
		}
	}()
	if err := btService.Restore(cmd.Context()); err != nil {
		return fmt.Errorf("restore BitTorrent tasks: %w", err)
	}

	address := os.Getenv("SERVER_ADDR")
	if address == "" {
		address = ":8080"
	}

	server := &http.Server{
		Addr:              address,
		Handler:           router.NewWithServices(db, btService, storageService),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("server listening on %s", address)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server failed: %w", err)
		}
		return nil
	case <-cmd.Context().Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}
	return nil
}

func ensureWritableDirectory(path string) error {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return fmt.Errorf("create download directory: %w", err)
	}
	probe, err := os.CreateTemp(path, ".home-gateway-write-test-*")
	if err != nil {
		return fmt.Errorf("download directory is not writable: %w", err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		return fmt.Errorf("close download directory probe: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("remove download directory probe: %w", err)
	}
	absolute, err := filepath.Abs(path)
	if err == nil {
		log.Printf("BitTorrent downloads stored under %s", absolute)
	}
	return nil
}
