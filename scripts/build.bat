@echo off
setlocal enabledelayedexpansion
chcp 65001 >nul

:: ================================================================
:: build.bat - Windows one-click build script
:: Builds doc-ai-gateway.exe and packages a distributable zip
:: ================================================================

set "SCRIPT_DIR=%~dp0"
set "ROOT_DIR=%SCRIPT_DIR%..\"
cd /d "%ROOT_DIR%"

echo ================================================================
echo  DocAI Gateway - Windows Build
echo ================================================================

:: --- Check Go ---
where go >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go is not installed.
    echo Please download and install Go from https://go.dev/dl/
    exit /b 1
)

for /f "tokens=3" %%i in ('go version') do set "GO_VER=%%i"
echo [INFO] Go version: %GO_VER%

:: --- Download dependencies ---
echo.
echo === Step 1: Download dependencies ===
set GOPROXY=https://goproxy.cn,direct
go mod download
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Failed to download dependencies.
    exit /b 1
)
echo [OK] Dependencies ready.

:: --- Build ---
echo.
echo === Step 2: Build doc-ai-gateway.exe ===

mkdir dist 2>nul

set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64

echo   Building...
go build -mod=mod -ldflags="-s -w -H windowsgui" -o dist/doc-ai-gateway.exe .
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Build failed.
    exit /b 1
)
echo [OK] dist/doc-ai-gateway.exe built.

:: --- Package ---
echo.
echo === Step 3: Package for distribution ===

set "PKG_DIR=%ROOT_DIR%dist\doc-ai-gateway-windows-amd64"

:: Clean old directory
if exist "%PKG_DIR%" rmdir /S /Q "%PKG_DIR%"
mkdir "%PKG_DIR%"

:: Copy exe
copy /Y "%ROOT_DIR%dist\doc-ai-gateway.exe" "%PKG_DIR%\" >nul

:: Copy config
copy /Y "%ROOT_DIR%cfg.yml" "%PKG_DIR%\" >nul 2>&1

:: Copy Chinese README (separate file avoids encoding issues in batch)
echo   Copying README...
copy /Y "%ROOT_DIR%scripts\windows-readme.txt" "%PKG_DIR%\README.txt" >nul

:: Package as zip
set "ZIP_NAME=%ROOT_DIR%dist\doc-ai-gateway-windows-amd64.zip"
if exist "%ZIP_NAME%" del /Q "%ZIP_NAME%"

powershell -Command "& { Compress-Archive -Path '%PKG_DIR%\*' -DestinationPath '%ZIP_NAME%' -Force; }"
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Packaging failed.
    exit /b 1
)

:: Get file size
for %%A in ("%ZIP_NAME%") do set "ZIP_SIZE=%%~zA"
set /a ZIP_SIZE_MB=%ZIP_SIZE%/1048576

echo.
echo ================================================================
echo Build complete!
echo   Output: %ZIP_NAME%  (~%ZIP_SIZE_MB% MB)
echo.
echo Contents:
echo   doc-ai-gateway.exe
echo   cfg.yml
echo   README.txt
echo.
echo Distribute the zip file to Windows users.
echo They just need to extract and double-click doc-ai-gateway.exe
echo ================================================================

:: Clean up intermediate directory
if exist "%PKG_DIR%" rmdir /S /Q "%PKG_DIR%"
echo [INFO] Intermediate files cleaned up.

endlocal