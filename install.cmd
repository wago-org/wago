@echo off
setlocal EnableExtensions EnableDelayedExpansion

rem Wago installer bootstrap for native Windows Command Prompt.
rem Downloads, verifies, and launches the native Wago installer, then refreshes
rem this Command Prompt's PATH when requested.

set "path_refresh_owner="
if defined WAGO_PATH_REFRESH_FILE (
  set "path_refresh_file=!WAGO_PATH_REFRESH_FILE!"
  set "path_refresh_owner=caller"
) else (
  set "path_refresh_file=%TEMP%\wago-refresh-!RANDOM!-!RANDOM!-!RANDOM!.request"
  set "WAGO_PATH_REFRESH_FILE=!path_refresh_file!"
)
del /f /q "!path_refresh_file!" >nul 2>&1

if defined WAGO_INSTALLER (
  if not exist "%WAGO_INSTALLER%" (
    echo wago: WAGO_INSTALLER does not exist: %WAGO_INSTALLER%>&2
    exit /b 1
  )
  "%WAGO_INSTALLER%" install %*
  set "installer_status=!ERRORLEVEL!"
  if "!installer_status!"=="2" (
    echo wago: this installer release predates the native install flow; wait for the channel to update and try again>&2
    exit /b 1
  )
  if not "!installer_status!"=="0" exit /b !installer_status!
  goto success
)

set "version=main"
if defined WAGO_VERSION set "version=%WAGO_VERSION%"
set "release_repo=wago-org/wago"
if defined WAGO_RELEASE_REPO set "release_repo=%WAGO_RELEASE_REPO%"
set "release_api=https://api.github.com/repos/!release_repo!/releases"
if defined WAGO_RELEASES_API_URL set "release_api=%WAGO_RELEASES_API_URL%"
set "release_download_base=https://github.com/!release_repo!/releases"
if defined WAGO_RELEASE_DOWNLOAD_BASE set "release_download_base=%WAGO_RELEASE_DOWNLOAD_BASE%"

where curl.exe >nul 2>&1
if errorlevel 1 goto unavailable
where certutil.exe >nul 2>&1
if errorlevel 1 goto unavailable

set "tmp_dir=%TEMP%\wago-install-!RANDOM!-!RANDOM!-!RANDOM!"
mkdir "!tmp_dir!" >nul 2>&1
if not exist "!tmp_dir!" (
  echo wago: could not create a temporary directory>&2
  exit /b 1
)

call :target
if errorlevel 1 (
  call :cleanup
  echo wago: this Windows architecture is not supported>&2
  exit /b 1
)
call :resolve_release
if errorlevel 1 goto unavailable_cleanup

set "asset=wago-installer-windows-!arch!"
set "url=!release_download_base!/download/!tag!/!asset!"
curl.exe -fsSL --retry 2 --connect-timeout 10 "!url!" -o "!tmp_dir!\installer.exe" >nul 2>&1
if errorlevel 1 goto unavailable_cleanup
curl.exe -fsSL --retry 2 --connect-timeout 10 "!url!.sha256" -o "!tmp_dir!\installer.sha256" >nul 2>&1
if errorlevel 1 goto unavailable_cleanup
call :verify_checksum
if errorlevel 1 (
  call :cleanup
  echo wago: the downloaded installer could not be verified; try again when the release service is available>&2
  exit /b 1
)

"!tmp_dir!\installer.exe" install %*
set "installer_status=!ERRORLEVEL!"
call :cleanup
if "!installer_status!"=="2" (
  echo wago: this installer release predates the native install flow; wait for the channel to update and try again>&2
  exit /b 1
)
if not "!installer_status!"=="0" exit /b !installer_status!
goto success

:success
if exist "!path_refresh_file!" if not defined path_refresh_owner call :refresh_path
if not defined path_refresh_owner del /f /q "!path_refresh_file!" >nul 2>&1
if defined refreshed_path for /f "delims=" %%P in ("!refreshed_path!") do endlocal & set "PATH=%%P"
exit /b 0

:target
set "arch="
if /i "%PROCESSOR_ARCHITECTURE%"=="AMD64" set "arch=amd64"
if /i "%PROCESSOR_ARCHITECTURE%"=="ARM64" set "arch=arm64"
if /i "%PROCESSOR_ARCHITEW6432%"=="AMD64" set "arch=amd64"
if /i "%PROCESSOR_ARCHITEW6432%"=="ARM64" set "arch=arm64"
if not defined arch exit /b 1
exit /b 0

:resolve_release
set "tag="
if /i "!version!"=="latest" (
  curl.exe -fsSL "!release_api!/latest" -o "!tmp_dir!\release.json" >nul 2>&1
  if errorlevel 1 exit /b 1
  for /f "usebackq tokens=1,* delims=:" %%A in ("!tmp_dir!\release.json") do (
    set "release_key=%%A"
    set "release_key=!release_key: =!"
    set "release_key=!release_key:"=!"
    if /i "!release_key!"=="tag_name" if not defined tag (
      call :clean_release_candidate "%%B"
      set "tag=!release_candidate!"
    )
  )
  if not defined tag exit /b 1
  exit /b 0
)
if /i "!version:~0,1!"=="v" set "tag=!version!"
if /i "!version:~0,7!"=="canary-" set "tag=!version!"
if /i "!version:~0,8!"=="nightly-" set "tag=!version!"
if defined tag exit /b 0
set "channel=canary"
if /i "!version!"=="nightly" set "channel=nightly"
curl.exe -fsSL "!release_api!?per_page=100" -o "!tmp_dir!\releases.json" >nul 2>&1
if errorlevel 1 exit /b 1
set "release_pending_tag="
set "release_best_tag="
set "release_best_published="
for /f "usebackq tokens=1,* delims=:" %%A in ("!tmp_dir!\releases.json") do (
  set "release_key=%%A"
  set "release_key=!release_key: =!"
  set "release_key=!release_key:"=!"
  if /i "!release_key!"=="tag_name" (
    call :clean_release_candidate "%%B"
    set "release_pending_tag="
    if /i "!channel!"=="canary" if /i "!release_candidate:~0,7!"=="canary-" set "release_pending_tag=!release_candidate!"
    if /i "!channel!"=="nightly" if /i "!release_candidate:~0,8!"=="nightly-" set "release_pending_tag=!release_candidate!"
  )
  if /i "!release_key!"=="published_at" if defined release_pending_tag (
    call :clean_release_candidate "%%B"
    if not defined release_best_published (
      set "release_best_published=!release_candidate!"
      set "release_best_tag=!release_pending_tag!"
    ) else if "!release_candidate!" GTR "!release_best_published!" (
      set "release_best_published=!release_candidate!"
      set "release_best_tag=!release_pending_tag!"
    )
    set "release_pending_tag="
  )
)
set "tag=!release_best_tag!"
if not defined tag exit /b 1
exit /b 0

:clean_release_candidate
set "release_candidate=%~1"
set "release_candidate=!release_candidate: =!"
set "release_candidate=!release_candidate:,=!"
set "release_candidate=!release_candidate:"=!"
exit /b 0

:verify_checksum
set "expected_hash="
for /f "usebackq tokens=1" %%H in ("!tmp_dir!\installer.sha256") do if not defined expected_hash set "expected_hash=%%H"
if not defined expected_hash exit /b 1
certutil.exe -hashfile "!tmp_dir!\installer.exe" SHA256 >"!tmp_dir!\installer.hash" 2>nul
if errorlevel 1 exit /b 1
set "actual_hash="
for /f "usebackq skip=1 tokens=*" %%H in ("!tmp_dir!\installer.hash") do if not defined actual_hash set "actual_hash=%%H"
set "actual_hash=!actual_hash: =!"
if /i not "!actual_hash!"=="!expected_hash!" exit /b 1
exit /b 0

:unavailable_cleanup
call :cleanup
:unavailable
echo wago: the installer is unavailable; check your internet connection and try again>&2
exit /b 1

:cleanup
if defined tmp_dir if exist "!tmp_dir!" rmdir /s /q "!tmp_dir!"
exit /b 0

:refresh_path
rem Adapted from Chocolatey's RefreshEnv.cmd. Read both registry PATH values
rem through %%WinDir%% so Windows may live on any drive.
rem https://github.com/chocolatey/choco/blob/develop/src/chocolatey.resources/redirects/RefreshEnv.cmd
set "machine_path="
set "user_path="
if defined WAGO_TEST_MACHINE_PATH (
  set "machine_path=!WAGO_TEST_MACHINE_PATH!"
) else (
  "!WinDir!\System32\reg.exe" query "HKLM\System\CurrentControlSet\Control\Session Manager\Environment" /v Path >"!path_refresh_file!.machine" 2>nul
  for /f "usebackq skip=2 tokens=2,*" %%A in ("!path_refresh_file!.machine") do set "machine_path=%%B"
)
if defined WAGO_TEST_USER_PATH (
  set "user_path=!WAGO_TEST_USER_PATH!"
) else (
  "!WinDir!\System32\reg.exe" query "HKCU\Environment" /v Path >"!path_refresh_file!.user" 2>nul
  for /f "usebackq skip=2 tokens=2,*" %%A in ("!path_refresh_file!.user") do set "user_path=%%B"
)
del /f /q "!path_refresh_file!.machine" "!path_refresh_file!.user" >nul 2>&1
call set "refreshed_path=%%machine_path%%;%%user_path%%"
exit /b 0
