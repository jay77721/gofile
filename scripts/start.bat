@echo off
REM FileStore Server - Local Development Startup Script (Windows)
REM
REM Usage:
REM   scripts\start.bat              start with .env file
REM   scripts\start.bat --migrate    run migrations then start
REM   scripts\start.bat --build      build binary then run

setlocal

set "PROJECT_DIR=%~dp0.."
cd /d "%PROJECT_DIR%"

REM Load .env file if present
if exist .env (
    echo Loading environment from .env...
    for /f "usebackq tokens=*" %%a in (`.env`) do (
        REM Skip comments and empty lines
        echo %%a | findstr /b "#" >nul || (
            for /f "tokens=1,* delims==" %%b in ("%%a") do (
                set "%%b=%%c"
            )
        )
    )
)

REM Handle flags
if "%~1"=="--migrate" goto :migrate
if "%~1"=="--build" goto :build

:run
echo Starting FileStore Server...
echo   Server:    %SERVER_ADDR%
echo   MySQL:     %MYSQL_DSN%
echo   MinIO:     %MINIO_ENDPOINT%
echo.
go run main.go
goto :eof

:migrate
echo Running database migrations...
if "%MYSQL_DSN%"=="" (
    echo Error: MYSQL_DSN is not set
    exit /b 1
)
for %%f in (migrations\*.up.sql) do (
    echo   Applying %%f...
    mysql -h 127.0.0.1 -P 3306 -u root -p root gofile < %%f 2>nul || (
        echo   Warning: migration %%f may have already been applied
    )
)
echo Migrations complete.
goto :eof

:build
echo Building...
go build -o gofile.exe .
echo Starting...
gofile.exe
goto :eof
