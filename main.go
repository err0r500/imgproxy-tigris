package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	S3Bucket        string
	S3Folder        string
	TigrisProxyBind string
}

func main() {
	cfg := Config{
		S3Bucket:        os.Getenv("S3_BUCKET"),
		S3Folder:        os.Getenv("S3_FOLDER"),
		TigrisProxyBind: os.Getenv("IMGPROXY_BIND"),
	}
	if cfg.S3Bucket == "" {
		slog.Error("Missing required environment variable(s)", "config", cfg)
		os.Exit(1)
	}
	if cfg.TigrisProxyBind == "" {
		cfg.TigrisProxyBind = ":8080"
	}

	// Initialize the proxy
    target, err := url.Parse("http://127.0.0.1:8081")
	if err != nil {
		slog.Error("Failed to parse imgproxy local endpoint", "error", err)
		os.Exit(1)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

	uploader := manager.NewUploader(initS3Client(), func(u *manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024
		u.BufferProvider = manager.NewBufferedReadSeekerWriteToPool(10 * 1024 * 1024)
	})

	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode == http.StatusOK {
			var buf bytes.Buffer
			teeReader := io.TeeReader(resp.Body, &buf)

			go func() {
				if err := uploadToS3(context.Background(), uploader, cfg, &buf, resp.Request.URL.Path); err != nil {
					slog.Error("S3 upload failed", "error", err)
				}
			}()

			resp.Body = io.NopCloser(teeReader)
		}
		return nil
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})

	if err := http.ListenAndServe(fmt.Sprintf("%s", cfg.TigrisProxyBind), nil); err != nil {
		slog.Error("Server failed", "error", err)
	}
}

func uploadToS3(ctx context.Context, uploader *manager.Uploader, cfg Config, r io.Reader, path string) error {
	key := generateS3Key(path)

	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(cfg.S3Bucket),
		Key:    aws.String(fmt.Sprintf("%s%s", cfg.S3Folder, key)),
		Body:   r,
	})

	if err != nil {
		slog.Error("Upload failed", "path", path, "key", key, "error", err)
		return err
	}

	slog.Info("Uploaded to S3", "path", path, "bucket", cfg.S3Bucket, "key", key)
	return nil
}

// generateS3Key creates a hash from the imgproxy URL path
func generateS3Key(path string) string {
	hash := md5.Sum([]byte(path))
	return hex.EncodeToString(hash[:])
}

func initS3Client() *s3.Client {
	sdkConfig, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		slog.Error("Failed to initialize AWS config", "error", err)
		os.Exit(1)
	}

	svc := s3.NewFromConfig(sdkConfig, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("https://fly.storage.tigris.dev")
		o.Region = "auto"
		o.UsePathStyle = true
	})

	return svc
}
