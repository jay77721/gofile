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
  - Cookie-based session authentication
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
│   └── user.go          # User management handlers
├── meta/                # File metadata management
│   └── filemeta.go      # File metadata structure and DB bridge
├── rd/                  # Redis operations
│   └── redis.go         # Redis connection and cache operations
├── util/                # Utility functions
│   ├── util.go          # Hash utilities (SHA1, MD5)
│   ├── chunk.go         # Chunk upload utilities
│   └── resp.go          # JSON response helper
├── static/              # Static files (frontend assets)
├── uploads/             # Uploaded files storage
└── go.mod               # Go module definition
```

## 🛠️ Technology Stack

- **Language**: Go 1.25.0
- **Database**: MySQL
- **Cache**: Redis
- **Web Framework**: net/http (standard library)
- **Authentication**: Cookie-based session with bcrypt

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

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | `/user/signup` | No | User registration |
| POST | `/user/signin` | No | User login |
| GET | `/user/info` | Yes | Get user information |

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
   ```sql
   CREATE TABLE tbl_file (
     file_sha1 char(40) NOT NULL PRIMARY KEY,
     file_name varchar(256) NOT NULL DEFAULT '',
     file_size bigint(20) DEFAULT 0,
     file_addr varchar(512) DEFAULT '',
     create_at datetime DEFAULT CURRENT_TIMESTAMP,
     status tinyint(4) NOT NULL DEFAULT 0 COMMENT '0-normal, 1-deleted, 2-forbidden'
   ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

   CREATE TABLE tbl_user (
     user_name varchar(64) NOT NULL PRIMARY KEY,
     user_pwd varchar(60) NOT NULL DEFAULT '' COMMENT 'bcrypt hash',
     signup_at datetime DEFAULT CURRENT_TIMESTAMP,
     status tinyint(4) NOT NULL DEFAULT 0
   ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

   CREATE TABLE tbl_user_token (
     user_name varchar(64) NOT NULL PRIMARY KEY,
     user_token char(64) NOT NULL DEFAULT '',
     update_at datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
     expired_at datetime
   ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
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
- **Input Validation**: File size limits, parameter validation
- **Soft Delete**: Files are marked as deleted, not physically removed

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
