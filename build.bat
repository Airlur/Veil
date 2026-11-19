@echo off
setlocal
set PATH=D:\Software\Dev\Go\bin;%PATH%

echo Building Veil...
go build -trimpath -ldflags "-H=windowsgui -s -w" -o Veil.exe .
if errorlevel 1 (
    echo.
    echo BUILD FAILED
    pause
    exit /b 1
)

rem go build 覆盖旧 exe 时会在同目录留下 Veil.exe~ 备份，删掉
if exist Veil.exe~ del Veil.exe~

echo.
echo BUILD OK
for %%F in (Veil.exe) do echo Output: %%~fF  (%%~zF bytes)

rem ---- dist 打包：产出绿色版目录 + 分发 zip ----
if /i "%1"=="dist" (
    if exist dist rmdir /s /q dist
    if exist Veil-windows.zip del Veil-windows.zip
    mkdir dist\presets
    copy /y Veil.exe dist\ >nul
    copy /y README.md dist\ >nul
    > dist\presets\README.txt echo(
    >> dist\presets\README.txt echo 这个文件夹用于目录预设：把任意 JPG/PNG/WEBP 图片放进来，
    >> dist\presets\README.txt echo 重启 Veil 后会自动出现在设置面板的预设网格里。
    >> dist\presets\README.txt echo 注意：不要放来源不明的版权素材，分发时也请自行确认。
    powershell -NoProfile -Command "Compress-Archive -Path dist\* -DestinationPath Veil-windows.zip -Force"
    echo DIST OK  -^> dist\
    echo ZIP  OK  -^> Veil-windows.zip（直接发给对方即可）
)

endlocal
