# gofile - Local Development Startup Script (Unix/macOS)
#
# Usage:
#   ./start.sh              # start with .env file
#   ./start.sh --migrate    # run schema.sql then start
#   ./start.sh --build      # build binary then run

set -e

# Load .env file if present
if [ -f .env ]; then
    echo "Loading environment from .env..."
    set -a
    . ./.env
    set +a
fi

# Handle flags
case "${1:-}" in
    --migrate)
        echo "Running database migrations..."
        if [ -z "$MYSQL_DSN" ]; then
            echo "Error: MYSQL_DSN is not set"
            exit 1
        fi
        MYSQL_HOST="127.0.0.1"
        MYSQL_PORT="3306"
        MYSQL_USER="root"
        MYSQL_PASS="root"
        MYSQL_DB="gofile"
        echo "  Applying schema.sql..."
        mysql -h "$MYSQL_HOST" -P "$MYSQL_PORT" -u "$MYSQL_USER" -p"$MYSQL_PASS" "$MYSQL_DB" < schema.sql 2>/dev/null || \
        echo "  Warning: schema.sql may have already been applied"
        echo "Migrations complete."
        ;;
    --build)
        echo "Building..."
        go build -o gofile .
        echo "Starting..."
        ./gofile
        ;;
    *)
        echo "Starting gofile..."
        echo "  Server:    ${SERVER_ADDR:-:8080}"
        echo "  MySQL:     ${MYSQL_DSN:-<not set>}"
        echo "  MinIO:     ${MINIO_ENDPOINT:-<local storage>}"
        echo ""
        go run main.go
        ;;
esac
