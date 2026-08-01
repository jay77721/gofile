@echo off
REM gofile - Local Development Startup Script (Windows)
REM
REM Usage:
REM   start.bat              start with .env file
REM   start.bat --migrate    run schema.sql then start
REM   start.bat --build      build binary then run

setlocal

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
echo Starting gofile...
echo   Server:    %SERVER_ADDR%
echo   MySQL:     %MYSQL_DSN%
echo   MinIO:     %MINIO_ENDPOINT%
echo.
go run main.go
goto :eof

:migrate
echo Running database migrations...
echo   Applying schema.sql...
mysql -h 127.0.0.1 -P 3306 -u root -p root gofile < schema.sql 2>nul || (
    echo   Warning: schema.sql may have already been applied
)
echo Migrations complete.
goto :eof

:build
echo Building...
go build -o gofile.exe .
echo Starting...
gofile.exe
goto :eof
