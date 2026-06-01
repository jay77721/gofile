# FileStore Server [中文](README_ZH.md)

A lightweight file storage server built with Go, supporting file upload, download, user management, and chunked uploads.

## 🚀 Features

- 📁 **File Management**
  - Upload files with metadata (100MB size limit)
  - Download files
  - Update file metadata
  - Delete files (soft delete)
  - Query file information

- 🔐 **User Authentication**
  - User registration (signup)
  - User login (signin)
  - User information retrieval
  - Cookie-based session authentication (SameSite=Strict)
  - bcrypt password hashing

- ⚡ **Chunked Upload**
  - Support for large file uploads
  - Chunk status checking
  - Automatic chunk merging
  - Instant upload (deduplication by hash)

- 🗄️ **Storage Backend**
  - MySQL database for metadata
  - Redis for caching and chunk tracking
  - Local file system storage

- 🛡️ **Security**
  - bcrypt password hashing
  - Secure random token generation (crypto/rand)
  - Path traversal protection (filepath.Base)
  - All file endpoints require authentication
  - Input validation and size limits
  - IP-based rate limiting on login/signup

- 📊 **Observability**
  - Structured JSON logging (log/slog)
  - Health check endpoint (/healthz)
  - Graceful shutdown (SIGINT/SIGTERM)

## 🏗️ Architecture

```
filestore-server/
├── main.go              # Entry point, HTTP routing, graceful shutdown
├── config/
│   └── config.go        # Environment-based configuration
├── db/                  # Database operations
│   ├── mysql/conn.go    # MySQL connection pool
│   ├── file.go          # File-related DB operations
│   └── user.go          # User-related DB operations
├── handler/             # HTTP request handlers
│   ├── auth.go          # Authentication middleware
│   ├── handler.go       # File upload/download handlers
│   ├── user.go          # User management handlers
│   └── ratelimit.go     # IP-based rate limiting
├── meta/                # File metadata management
│   └── filemeta.go      # File metadata structure and DB bridge
├── rd/                  # Redis operations
│   └── redis.go         # Redis connection and cache operations
├── util/                # Utility functions
│   ├── util.go          # Hash utilities (SHA1, MD5)
│   ├── chunk.go         # Chunk upload utilities
│   └── resp.go          # JSON response helper
├── migrations/          # SQL migration scripts
├── static/              # Static files (frontend assets)
├── uploads/             # Uploaded files storage
├── Dockerfile           # Multi-stage Docker build
├── docker-compose.yml   # Docker Compose configuration
└── go.mod               # Go module definition
```

## 🛠️ Technology Stack

- **Language**: Go 1.25.0
- **Database**: MySQL
- **Cache**: Redis
- **Web Framework**: net/http (standard library)
- **Authentication**: Cookie-based session with bcrypt
- **Logging**: log/slog (structured JSON)
- **Container**: Docker + Docker Compose

## 📋 API Endpoints

### File Operations (🔒 Require Authentication)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/file/upload` | Upload a file |
| GET | `/file/meta` | Get file metadata |
| GET | `/file/query` | Query all files |
| GET | `/file/download` | Download a file |
| POST | `/file/update` | Update file metadata |
| POST | `/file/delete` | Delete a file (soft delete) |

### Chunk Upload (🔒 Require Authentication)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/file/upload/chunk` | Upload file chunk |
| GET | `/file/upload/status` | Check chunk upload status |
| POST | `/file/upload/merge` | Merge uploaded chunks |

### User Operations

| Method | Endpoint | Auth | Rate Limit | Description |
|--------|----------|------|------------|-------------|
| POST | `/user/signup` | No | Yes | User registration |
| POST | `/user/signin` | No | Yes | User login |
| GET | `/user/info` | Yes | No | Get user information |

### System

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/healthz` | Health check (MySQL + Redis) |

## 🚦 Getting Started

### Docker (Recommended)

```bash
# Clone and start with Docker Compose
git clone <repository-url>
cd filestore-server
docker compose up -d
```

The server will start at `http://localhost:8080` with MySQL and Redis automatically configured.

### Manual Installation

#### Prerequisites

- Go 1.25.0 or higher
- MySQL database
- Redis server

#### Installation

1. **Clone the repository**
   ```bash
   git clone <repository-url>
   cd filestore-server
   ```

2. **Install dependencies**
   ```bash
   go mod tidy
   ```

3. **Set environment variables**
   ```bash
   # MySQL connection string (required)
   export MYSQL_DSN="user:password@tcp(host:port)/dbname?charset=utf8mb4&parseTime=True&loc=Local"

   # Redis configuration (optional, defaults shown)
   export REDIS_ADDR="127.0.0.1:6379"
   export REDIS_PASS=""
   export REDIS_DB=0

   # Server configuration (optional, defaults shown)
   export SERVER_ADDR=":8080"
   export UPLOAD_DIR="./uploads"
   export CHUNK_DIR="./chunks"
   ```

4. **Create database tables**
   ```bash
   # Using migration scripts
   mysql -u root -p fileserver < migrations/000001_init_schema.up.sql
   mysql -u root -p fileserver < migrations/000002_add_indexes.up.sql
   ```

5. **Run the server**
   ```bash
   go run main.go
   ```

6. **Access the server**
   - Server will start on `http://localhost:8080`
   - Static files served from `/static/`
   - Health check at `http://localhost:8080/healthz`

## 📝 Usage Examples

### Upload a File
```bash
curl -X POST -F "file=@/path/to/your/file.txt" \
  -b "username=testuser;token=your_token" \
  http://localhost:8080/file/upload
```

### Download a File
```bash
curl -X GET "http://localhost:8080/file/download?filehash=abc123" \
  -b "username=testuser;token=your_token" \
  --output file.txt
```

### User Registration
```bash
curl -X POST -d "username=testuser&password=password123" \
  http://localhost:8080/user/signup
```

### User Login
```bash
curl -X POST -F "username=testuser&password=password123" \
  http://localhost:8080/user/signin
```

## ⚙️ Configuration

All configuration is done via environment variables with sensible defaults:

| Variable | Default | Description |
|----------|---------|-------------|
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/fileserver?...` | MySQL connection string |
| `REDIS_ADDR` | `127.0.0.1:6379` | Redis server address |
| `REDIS_PASS` | (empty) | Redis password |
| `REDIS_DB` | `0` | Redis database number |
| `SERVER_ADDR` | `:8080` | HTTP server listen address |
| `UPLOAD_DIR` | `./uploads` | File upload directory |
| `CHUNK_DIR` | `./chunks` | Chunk upload directory |

## 🔒 Security Features

- **Password Hashing**: bcrypt with default cost
- **Token Generation**: 32-byte random tokens via crypto/rand
- **Path Traversal Protection**: All filenames sanitized with filepath.Base
- **Authentication**: All file operations require valid session cookie
- **CSRF Protection**: Cookies set with SameSite=Strict
- **Rate Limiting**: IP-based token bucket on login/signup (5 req/s, burst 10)
- **Input Validation**: File size limits (100MB), parameter validation
- **Soft Delete**: Files are marked as deleted, not physically removed

## 🧪 Testing

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./util/...
go test ./handler/...

# Run with verbose output
go test -v ./...
```

## 📊 Logging

The server uses structured JSON logging via `log/slog`:

```json
{"time":"2025-01-01T12:00:00Z","level":"INFO","msg":"user logged in","username":"testuser"}
{"time":"2025-01-01T12:00:00Z","level":"WARN","msg":"login failed","username":"testuser","reason":"invalid credentials"}
{"time":"2025-01-01T12:00:00Z","level":"ERROR","msg":"prepare statement failed","error":"...","op":"checkPassword"}
```

## 🚧 Development Status

This project is under active development. Features may change and APIs are subject to modification.

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## 📄 License

This project is for educational and learning purposes.

## 🆘 Support

For issues and questions, please open an issue in the repository.
