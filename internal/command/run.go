package command

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	appconfig "home-gateway/internal/config"
	"home-gateway/internal/dns"
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

	storageService := storage.NewService(config.Storage.Backends)
	syncScheduler := storage.NewScheduler(storageService)
	storageService.SetScheduler(syncScheduler)
	if err := syncScheduler.Replace(config.Storage.Sync); err != nil {
		return fmt.Errorf("start storage sync scheduler: %w", err)
	}
	defer syncScheduler.Stop()
	dnsService := dns.NewService(config.DNS.Cloudflare)

	reload := func() error {
		reloaded, err := appconfig.Load(configPath, true)
		if err != nil {
			return err
		}
		storageService.Replace(reloaded.Storage.Backends)
		if err := syncScheduler.Replace(reloaded.Storage.Sync); err != nil {
			return err
		}
		dnsService.Replace(reloaded.DNS.Cloudflare)
		log.Printf("configuration reloaded from %s", configPath)
		return nil
	}

	address := os.Getenv("SERVER_ADDR")
	if address == "" {
		address = ":8080"
	}

	server := &http.Server{
		Addr: address,
		Handler: router.NewWithServices(router.Services{
			Database: db,
			Storage:  storageService,
			DNS:      dnsService,
			Reload:   reload,
		}),
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
