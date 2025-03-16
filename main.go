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
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	//"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
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

	s3Client := initS3Client(cfg)

	// Initialize the proxy
	target, err := url.Parse(cfg.ImgproxyEndpoint)
	if err != nil {
		slog.Error("Failed to parse imgproxy endpoint", "error", err)
		os.Exit(1)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Add response modifier for S3 upload
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Only process successful responses
		if resp.StatusCode == http.StatusOK {
			// Read the entire response body
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				slog.Error("Failed to read response body", "error", err)
				return err
			}
			resp.Body.Close()

			// Start S3 upload in a goroutine as best-effort
			go func() {
				ctx := context.Background()
				if err := uploadToS3(ctx, s3Client, cfg, bytes.NewReader(body), resp.Request.URL.Path); err != nil {
					slog.Error("S3 upload failed", "error", err)
				}
			}()

			// Set the response body to a new reader with the cached content
			resp.Body = io.NopCloser(bytes.NewReader(body))
		}
		return nil
	}

	http.HandleFunc("/", proxyHandler(s3Client, proxy))
	slog.Info("Starting proxy server", "port", os.Getenv("PROXY_HTTP_PORT"))
	err = http.ListenAndServe(fmt.Sprintf(":%s", os.Getenv("PROXY_HTTP_PORT")), nil)
	if err != nil {
		slog.Error("Server failed", "error", err)
	}
}

func proxyHandler(s3Client *s3.Client, proxy *httputil.ReverseProxy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.Info("Proxying request", "path", r.URL.Path)
		proxy.ServeHTTP(w, r)
	}
}

// generateS3Key creates a hash from the imgproxy URL parts after the signature
func generateS3Key(path string) string {
	// Split path into components
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 3 {
		return ""
	}

	// Everything after signature (processing options and source URL)
	toHash := strings.Join(parts[1:], "/")

	// Generate MD5 hash
	hash := md5.Sum([]byte(toHash))
	return hex.EncodeToString(hash[:])
}

func uploadToS3(ctx context.Context, client *s3.Client, cfg Config, r io.Reader, path string) error {
	// Generate key from URL path
	key := generateS3Key(path)
	if key == "" {
		return fmt.Errorf("invalid URL path format")
	}

	slog.Info("Uploading to S3", "path", path, "key", key)
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(cfg.S3Bucket),
		Key:    aws.String(key),
		Body:   r,
	})

	return err
}

func initS3Client(cfg Config) *s3.Client {
	customResolver := aws.EndpointResolverWithOptionsFunc(
		func(service, region string, opts ...interface{}) (aws.Endpoint, error) {
			if cfg.S3Endpoint != "" {
				return aws.Endpoint{
					PartitionID:   "aws",
					URL:           cfg.S3Endpoint,
					SigningRegion: cfg.S3Region,
				}, nil
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		},
	)

	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithEndpointResolverWithOptions(customResolver),
		config.WithRegion(cfg.S3Region),
	)
	if err != nil {
		slog.Error("Failed to initialize AWS config", "error", err)
		os.Exit(1)
	}

	// Create S3 client with path-style addressing required for LocalStack
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})
}
