@echo off
setlocal EnableExtensions EnableDelayedExpansion
if /i "%~nx0"=="wago-pipe.cmd" set "WAGO_CMD_PIPE=1"

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
call :try_download "!tag_1!"
set "download_status=!ERRORLEVEL!"
if "!download_status!"=="1" call :try_download "!tag_2!"
if "!download_status!"=="1" set "download_status=!ERRORLEVEL!"
if "!download_status!"=="1" call :try_download "!tag_3!"
if "!download_status!"=="1" set "download_status=!ERRORLEVEL!"
if "!download_status!"=="2" (
  call :cleanup
  echo wago: the downloaded installer could not be verified; try again when the release service is available>&2
  exit /b 1
)
if not "!download_status!"=="0" goto unavailable_cleanup

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
set "tag_1="
set "tag_2="
set "tag_3="
if /i "!version!"=="latest" (
  call :resolve_latest
  exit /b !ERRORLEVEL!
)
if /i "!version:~0,1!"=="v" set "tag_1=!version!"
if defined tag_1 exit /b 0
if /i not "!version!"=="main" if /i not "!version!"=="beta" if /i not "!version!"=="canary" exit /b 1
if /i "!version!"=="main" (
  call :resolve_latest
  if defined tag_1 (
    set "release_core=!tag_1!"
    call :validate_release_core
    if not defined release_core_valid set "tag_1="
  )
)
set "release_pending_tag="
set "release_pending_channel="
for %%C in (beta canary) do (
  set "release_best_%%C_tag="
  set "release_best_%%C_published="
)
set "release_page=1"
:release_page_loop
curl.exe -fsSL "!release_api!?per_page=100&page=!release_page!" -o "!tmp_dir!\releases.json" >nul 2>&1
if errorlevel 1 goto release_pages_done
set "release_count=0"
for /f "usebackq tokens=1,* delims=:" %%A in ("!tmp_dir!\releases.json") do (
  set "release_key=%%A"
  set "release_key=!release_key: =!"
  set "release_key=!release_key:"=!"
  if /i "!release_key!"=="tag_name" (
    set /a release_count+=1
    call :clean_release_candidate "%%B"
    set "release_pending_tag="
    set "release_pending_channel="
    call :classify_release_candidate
    if defined release_pending_channel set "release_pending_tag=!release_candidate!"
  )
  if /i "!release_key!"=="draft" (
    call :clean_release_candidate "%%B"
    if /i "!release_candidate!"=="true" set "release_pending_tag="
  )
  if /i "!release_key!"=="published_at" if defined release_pending_tag (
    call :clean_release_candidate "%%B"
    call :remember_release "!release_pending_channel!" "!release_pending_tag!" "!release_candidate!"
    set "release_pending_tag="
    set "release_pending_channel="
  )
)
if /i "!version!"=="beta" if defined release_best_beta_tag goto release_pages_done
if /i "!version!"=="canary" if defined release_best_canary_tag goto release_pages_done
if /i "!version!"=="main" if defined release_best_beta_tag if defined release_best_canary_tag goto release_pages_done
if !release_count! LSS 100 goto release_pages_done
set /a release_page+=1
if !release_page! LEQ 10 goto release_page_loop
:release_pages_done
if /i "!version!"=="main" (
  set "tag_2=!release_best_beta_tag!"
  set "tag_3=!release_best_canary_tag!"
) else if /i "!version!"=="beta" (
  set "tag_1=!release_best_beta_tag!"
) else (
  set "tag_1=!release_best_canary_tag!"
)
if not defined tag_1 if not defined tag_2 if not defined tag_3 exit /b 1
exit /b 0

:resolve_latest
set "tag_1="
curl.exe -fsSL "!release_api!/latest" -o "!tmp_dir!\release.json" >nul 2>&1
if errorlevel 1 exit /b 1
for /f "usebackq tokens=1,* delims=:" %%A in ("!tmp_dir!\release.json") do (
  set "release_key=%%A"
  set "release_key=!release_key: =!"
  set "release_key=!release_key:"=!"
  if /i "!release_key!"=="tag_name" if not defined tag_1 (
    call :clean_release_candidate "%%B"
    set "tag_1=!release_candidate!"
  )
)
if not defined tag_1 exit /b 1
exit /b 0

:classify_release_candidate
set "release_pending_channel="
set "release_parts=!release_candidate:-beta.= !"
set "release_core="
set "release_identity="
set "release_extra="
for /f "tokens=1,2,3" %%A in ("!release_parts!") do (
  set "release_core=%%A"
  set "release_identity=%%B"
  set "release_extra=%%C"
)
if not "!release_parts!"=="!release_candidate!" if not defined release_extra if /i "!release_core!-beta.!release_identity!"=="!release_candidate!" (
  call :validate_release_core
  if defined release_core_valid (
    set "number_value=!release_identity!"
    call :validate_canonical_number
    if defined number_valid set "release_pending_channel=beta"
  )
)
if defined release_pending_channel exit /b 0
set "release_parts=!release_candidate:-canary.g= !"
set "release_core="
set "release_identity="
set "release_extra="
for /f "tokens=1,2,3" %%A in ("!release_parts!") do (
  set "release_core=%%A"
  set "release_identity=%%B"
  set "release_extra=%%C"
)
if not "!release_parts!"=="!release_candidate!" if not defined release_extra if /i "!release_core!-canary.g!release_identity!"=="!release_candidate!" (
  call :validate_release_core
  if defined release_core_valid (
    set "hex_value=!release_identity!"
    call :validate_short_hex
    if defined hex_valid set "release_pending_channel=canary"
  )
)
exit /b 0

:validate_release_core
set "release_core_valid="
set "release_core_parts=!release_core:.= !"
set "release_major="
set "release_minor="
set "release_patch="
set "release_extra="
for /f "tokens=1,2,3,4" %%A in ("!release_core_parts!") do (
  set "release_major=%%A"
  set "release_minor=%%B"
  set "release_patch=%%C"
  set "release_extra=%%D"
)
if /i not "!release_major:~0,1!"=="v" exit /b 0
set "release_major=!release_major:~1!"
if /i not "v!release_major!.!release_minor!.!release_patch!"=="!release_core!" exit /b 0
if defined release_extra exit /b 0
set "number_value=!release_major!"
call :validate_canonical_number
if not defined number_valid exit /b 0
set "number_value=!release_minor!"
call :validate_canonical_number
if not defined number_valid exit /b 0
set "number_value=!release_patch!"
call :validate_canonical_number
if not defined number_valid exit /b 0
set "release_core_valid=1"
exit /b 0

:validate_canonical_number
set "number_valid="
if not defined number_value exit /b 0
set "number_invalid="
for /f "delims=0123456789" %%N in ("!number_value!") do set "number_invalid=1"
if defined number_invalid exit /b 0
if "!number_value:~0,1!"=="0" if not "!number_value!"=="0" exit /b 0
set "number_valid=1"
exit /b 0

:validate_short_hex
set "hex_valid="
if "!hex_value:~6,1!"=="" exit /b 0
if not "!hex_value:~7,1!"=="" exit /b 0
set "hex_invalid="
for /f "delims=0123456789abcdefABCDEF" %%H in ("!hex_value!") do set "hex_invalid=1"
if defined hex_invalid exit /b 0
set "hex_valid=1"
exit /b 0

:remember_release
set "remember_channel=%~1"
set "remember_tag=%~2"
set "remember_published=%~3"
for %%C in (!remember_channel!) do (
  if not defined release_best_%%C_published (
    set "release_best_%%C_published=!remember_published!"
    set "release_best_%%C_tag=!remember_tag!"
  ) else if "!remember_published!" GTR "!release_best_%%C_published!" (
    set "release_best_%%C_published=!remember_published!"
    set "release_best_%%C_tag=!remember_tag!"
  )
)
exit /b 0

:try_download
set "download_tag=%~1"
if not defined download_tag exit /b 1
del /f /q "!tmp_dir!\installer.exe" "!tmp_dir!\installer.sha256" >nul 2>&1
set "url=!release_download_base!/download/!download_tag!/!asset!"
curl.exe -fsSL --retry 2 --connect-timeout 10 "!url!" -o "!tmp_dir!\installer.exe" >nul 2>&1
if errorlevel 1 exit /b 1
curl.exe -fsSL --retry 2 --connect-timeout 10 "!url!.sha256" -o "!tmp_dir!\installer.sha256" >nul 2>&1
if errorlevel 1 exit /b 1
call :verify_checksum
if errorlevel 1 exit /b 2
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
