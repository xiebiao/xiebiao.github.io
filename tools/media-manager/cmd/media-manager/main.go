package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/xiebiao/xiebiao.github.io/tools/media-manager/internal/manager"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	repository, err := manager.OpenRepository(config.database)
	if err != nil {
		return err
	}
	defer repository.Close()
	objects, err := manager.NewR2Store(manager.R2Config{
		Endpoint: config.endpoint, Bucket: config.bucket, AccessKeyID: config.accessKeyID,
		SecretAccessKey: config.secretAccessKey, PublicBaseURL: config.publicBaseURL,
	})
	if err != nil {
		return err
	}
	handler, err := manager.NewServer(manager.ServerOptions{
		Repository: repository, Objects: objects, Manifest: config.manifest,
	})
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", config.port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stopped
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Printf("media manager listening on http://%s", server.Addr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

type config struct {
	endpoint, bucket, accessKeyID, secretAccessKey, publicBaseURL string
	database, manifest                                            string
	port                                                          int
}

func loadConfig() (config, error) {
	accountID := os.Getenv("R2_ACCOUNT_ID")
	endpoint := os.Getenv("R2_ENDPOINT")
	if endpoint == "" && accountID != "" {
		endpoint = "https://" + accountID + ".r2.cloudflarestorage.com"
	}
	port := 7331
	if value := os.Getenv("MEDIA_MANAGER_PORT"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 65535 {
			return config{}, errors.New("MEDIA_MANAGER_PORT must be between 1 and 65535")
		}
		port = parsed
	}
	database := envOr("MEDIA_DATABASE", filepath.Join("var", "media.db"))
	manifest := envOr("MEDIA_MANIFEST", filepath.Join("..", "..", "data", "media", "assets.json"))
	result := config{
		endpoint: endpoint, bucket: os.Getenv("R2_BUCKET"), accessKeyID: os.Getenv("R2_ACCESS_KEY_ID"),
		secretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"), publicBaseURL: envOr("MEDIA_BASE_URL", "https://media.xiebiao.com"),
		database: database, manifest: manifest, port: port,
	}
	if result.endpoint == "" || result.bucket == "" || result.accessKeyID == "" || result.secretAccessKey == "" {
		return config{}, errors.New("R2_ACCOUNT_ID (or R2_ENDPOINT), R2_BUCKET, R2_ACCESS_KEY_ID, and R2_SECRET_ACCESS_KEY are required")
	}
	return result, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
