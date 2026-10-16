@echo off
setlocal enabledelayedexpansion

:: ================================================================
:: build.bat - Windows 一键编译脚本
:: 在 Windows 上编译 doc-ai-gateway.exe 并打包
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
go build -mod=mod -ldflags="-s -w" -o dist/doc-ai-gateway.exe .
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

:: Create default database
echo   Creating default database...
copy /Y "%ROOT_DIR%cfg.db.template" "%PKG_DIR%\cfg.db" >nul 2>&1
if not exist "%ROOT_DIR%cfg.db.template" (
    :: Create a minimal template - the app will initialize on first run
    type nul > "%PKG_DIR%\cfg.db"
)

:: Copy test data
if exist "%ROOT_DIR%testdata" xcopy /E /I /Q /Y "%ROOT_DIR%testdata" "%PKG_DIR%\testdata\" >nul

:: Create README
echo   Creating README...
(
    echo DocAI Gateway - 文档 AI 智能网关
    echo ====================================
    echo.
    echo 快速开始:
    echo   1. 双击 doc-ai-gateway.exe 启动服务
    echo   2. 打开浏览器访问 http://localhost:8080
    echo   3. 使用默认账号 admin/admin 登录
    echo.
    echo 目录说明:
    echo   doc-ai-gateway.exe  主程序
    echo   cfg.yml            配置文件
    echo   cfg.db             SQLite 数据库
    echo   data\              文件存储目录
    echo   testdata\          测试文件
    echo.
    echo 对外接口:
    echo   POST /api/files/upload   上传文件
    echo   GET  /api/files/status/:id  查询文件状态
    echo   POST /api/ai/call       调用 AI 接口
    echo.
    echo 管理后台: http://localhost:8080/admin
) > "%PKG_DIR%\README.txt"

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
echo   cfg.db
echo   README.txt
echo.
echo Distribute the zip file to Windows users.
echo They just need to extract and double-click doc-ai-gateway.exe
echo ================================================================

:: Clean up intermediate directory
if exist "%PKG_DIR%" rmdir /S /Q "%PKG_DIR%"
echo [INFO] Intermediate files cleaned up.

endlocal