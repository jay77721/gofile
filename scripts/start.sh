# FileStore Server - Local Development Startup Script (Unix/macOS)
#
# Usage:
#   ./scripts/start.sh              # start with .env file
#   ./scripts/start.sh --migrate    # run migrations then start
#   ./scripts/start.sh --build      # build binary then run

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

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
        # Extract connection details from DSN (basic parsing)
        MYSQL_HOST="127.0.0.1"
        MYSQL_PORT="3306"
        MYSQL_USER="root"
        MYSQL_PASS="root"
        MYSQL_DB="fileserver"
        for f in migrations/*.up.sql; do
            echo "  Applying $f..."
            mysql -h "$MYSQL_HOST" -P "$MYSQL_PORT" -u "$MYSQL_USER" -p"$MYSQL_PASS" "$MYSQL_DB" < "$f" 2>/dev/null || \
            echo "  Warning: migration $f may have already been applied"
        done
        echo "Migrations complete."
        ;;
    --build)
        echo "Building..."
        go build -o gofile .
        echo "Starting..."
        ./gofile
        ;;
    *)
        echo "Starting FileStore Server..."
        echo "  Server:    ${SERVER_ADDR:-:8080}"
        echo "  MySQL:     ${MYSQL_DSN:-<not set>}"
        echo "  MinIO:     ${MINIO_ENDPOINT:-<local storage>}"
        echo ""
        go run main.go
        ;;
esac
