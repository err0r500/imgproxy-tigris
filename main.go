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
	S3Endpoint       string
	S3Bucket         string
	S3Region         string
	ImgproxyEndpoint string
}

func main() {
	cfg := Config{
		S3Endpoint:       os.Getenv("PROXY_S3_ENDPOINT"),
		S3Bucket:         os.Getenv("PROXY_S3_BUCKET"),
		S3Region:         os.Getenv("PROXY_S3_REGION"),
		ImgproxyEndpoint: os.Getenv("PROXY_IMGPROXY_ENDPOINT"),
	}
	if cfg.S3Endpoint == "" || cfg.S3Bucket == "" || cfg.S3Region == "" {
		slog.Error("Missing required environment variable(s)", "config", cfg)
		os.Exit(1)
	}
	if cfg.ImgproxyEndpoint == "" {
		cfg.ImgproxyEndpoint = "http://localhost:8080"
	}

	uploader := manager.NewUploader(initS3Client(cfg), func(u *manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024
		u.BufferProvider = manager.NewBufferedReadSeekerWriteToPool(10 * 1024 * 1024)
	})

	// Initialize the proxy
	target, err := url.Parse(cfg.ImgproxyEndpoint)
	if err != nil {
		slog.Error("Failed to parse imgproxy endpoint", "error", err)
		os.Exit(1)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

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

	if err := http.ListenAndServe(fmt.Sprintf(":%s", os.Getenv("PROXY_HTTP_PORT")), nil); err != nil {
		slog.Error("Server failed", "error", err)
	}
}

func uploadToS3(ctx context.Context, uploader *manager.Uploader, cfg Config, r io.Reader, path string) error {
	key := generateS3Key(path)

	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(cfg.S3Bucket),
		Key:    aws.String(key),
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

func initS3Client(cfg Config) *s3.Client {
	sdkConfig, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		slog.Error("Failed to initialize AWS config", "error", err)
		os.Exit(1)
	}

	svc := s3.NewFromConfig(sdkConfig, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		o.Region = cfg.S3Region
		o.UsePathStyle = true
	})

	return svc
}
