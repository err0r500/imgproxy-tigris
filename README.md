# Imgproxy to Tigris Proxy

![CI Status](https://github.com/${{ github.repository }}/actions/workflows/ci.yml/badge.svg)

## Features
- Dual-process architecture (Go proxy + imgproxy)
- Non-blocking S3 writes with best-effort cleanup
- Namespaced environment configuration
- Structured error logging

## Security
- Automatic CVE scanning
- Distroless base image
- Signed containers

## Environment Variables
| Prefix | Purpose |
|--------|---------|
| `PROXY_*` | Go proxy configuration |
| `IMGPROXY_*` | Native imgproxy settings |

## Deployment
```bash
docker build -t imgproxy-tigris .

docker run -p 8080:8080 \
  -e PROXY_S3_ENDPOINT="your.tigris.endpoint" \
  -e PROXY_S3_BUCKET="your-bucket" \
  -e PROXY_S3_REGION="auto" \
  -e IMGPROXY_MAX_SRC_RESOLUTION=16384 \
  imgproxy-tigris
```

## Local Development
```bash
docker-compose -f docker-compose.local.yml up --build
aws --endpoint-url=http://localhost:4566 s3 mb s3://test-bucket
```

## Operational Notes
- S3 writes happen in parallel with client streaming
- Partial uploads are automatically cleaned up
- Process crashes are handled by supervisord
