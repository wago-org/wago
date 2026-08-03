@echo off
setlocal EnableExtensions EnableDelayedExpansion

rem Wago installer for native Windows Command Prompt.
rem
rem   curl.exe -fsSLo "%TEMP%\wago-install.cmd" https://raw.githubusercontent.com/wago-org/wago/main/install.cmd ^&^& call "%TEMP%\wago-install.cmd" ^&^& del "%TEMP%\wago-install.cmd"
rem
rem Downloads checksummed installer and manager executables from the selected
rem release channel and uses Go 1.22+ only when the manager is unavailable.
rem Git is preferred; curl.exe and tar.exe provide the source-archive fallback.
rem The installer never invokes another scripting host.

if not defined USERPROFILE (
  echo wago: USERPROFILE is not set>&2
  exit /b 1
)

set "repo_url=https://github.com/wago-org/wago.git"
if defined WAGO_REPO_URL set "repo_url=%WAGO_REPO_URL%"
set "version=main"
if defined WAGO_VERSION set "version=%WAGO_VERSION%"
set "archive_url=https://api.github.com/repos/wago-org/wago/zipball/!version!"
if defined WAGO_ARCHIVE_URL set "archive_url=%WAGO_ARCHIVE_URL%"
set "source_version=!version!"
set "source_archive_url=!archive_url!"
set "release_repo=wago-org/wago"
if defined WAGO_RELEASE_REPO set "release_repo=%WAGO_RELEASE_REPO%"
set "release_api=https://api.github.com/repos/!release_repo!/releases"
if defined WAGO_RELEASES_API_URL set "release_api=%WAGO_RELEASES_API_URL%"
set "release_download_base=https://github.com/!release_repo!/releases"
if defined WAGO_RELEASE_DOWNLOAD_BASE set "release_download_base=%WAGO_RELEASE_DOWNLOAD_BASE%"

set "bin_explicit=0"
if defined WAGO_BIN_DIR set "bin_explicit=1"
set "bin_dir=%USERPROFILE%\.wago\bin"
if defined WAGO_BIN_DIR set "bin_dir=%WAGO_BIN_DIR%"
set "src_dir=%USERPROFILE%\.wago\src"
if defined WAGO_SRC_DIR set "src_dir=%WAGO_SRC_DIR%"
set "default_wago_root=%USERPROFILE%\.wago"
set "wago_root=%USERPROFILE%\.wago"
if defined WAGO_HOME set "wago_root=%WAGO_HOME%"
set "wago_data=!wago_root!"
if defined WAGO_HOME set "wago_data=!wago_root!\data"
set "wago_config=!wago_root!\config"
set "wago_cache=!wago_root!\cache"
set "wago_exe=!bin_dir!\wago.exe"
set "tmp_dir="
set "ui_tmp="
set "ui_helper="
if defined WAGO_TUI_HELPER (
  set "ui_tmp=%TEMP%\wago-ui-!RANDOM!-!RANDOM!-!RANDOM!"
  mkdir "!ui_tmp!" >nul 2>&1
  set "ui_helper=%WAGO_TUI_HELPER%"
)
set "spinner_stop="
set "spinner_count=0"
set "reinstall_mode=minimal"

if "%WAGO_INTERNAL_VERIFY_ONLY%"=="1" goto verify_only
if "%WAGO_INTERNAL_FETCH_ONLY%"=="1" goto fetch_only
if "%WAGO_INTERNAL_MANAGER_ONLY%"=="1" goto manager_only
if "%WAGO_INTERNAL_INSTALLER_ONLY%"=="1" goto installer_only
if "%WAGO_INTERNAL_REINSTALL_CHECK_ONLY%"=="1" goto reinstall_check_only
if defined WAGO_INTERNAL_REINSTALL_CLEANUP_ONLY goto reinstall_cleanup_only
if "%WAGO_INTERNAL_PATH_SETUP_ONLY%"=="1" goto path_setup_only
if "%WAGO_INTERNAL_INSTALL_DIR_ONLY%"=="1" goto install_dir_only

:main
call :make_temp
if errorlevel 1 (
  call :die "could not create a temporary directory"
  exit /b 1
)
call :welcome
call :ensure_ui_helper
call :choose_install_dir
if errorlevel 1 (
  echo Cancelled.
  call :cleanup
  exit /b 0
)
call :report_install_dir

if "%WAGO_DRY_RUN%"=="1" (
  echo   version  !version!
  echo   command  !wago_exe!
  echo   source   !src_dir!
  echo No changes made.
  call :cleanup
  exit /b 0
)

call :choose_reinstall_mode
if errorlevel 1 (
  echo Cancelled.
  call :cleanup
  exit /b 0
)

set "manager_method=source"
set "manager_release_tag="
call :download_manager_release
if errorlevel 1 (
  call :ensure_ui_helper
  call :progress_begin "checking Go toolchain"
  where go.exe >nul 2>&1
  if errorlevel 1 (
    call :progress_fail "Go 1.22 or newer is required"
    call :die "could not download a release manager; install Go 1.22 or newer and run the installer again"
    exit /b 1
  )
  call :go_version_ok
  if errorlevel 1 (
    call :progress_fail "Go 1.22 or newer is required"
    call :die "could not download a release manager; install Go 1.22 or newer and run the installer again"
    exit /b 1
  )
  call :progress_done "Go toolchain ready"
) else (
  set "source_version=!manager_release_tag!"
  set "source_archive_url=https://api.github.com/repos/!release_repo!/zipball/!source_version!"
  if defined WAGO_ARCHIVE_URL set "source_archive_url=!WAGO_ARCHIVE_URL!"
)
call :fetch_source
if errorlevel 1 (
  call :die "could not fetch !repo_url! at !source_version!"
  exit /b 1
)

if "!manager_method!"=="release" (
  set "stamp=!manager_release_tag!"
) else (
  set "stamp=!version!"
  if "!source_method!"=="git" (
    for /f "usebackq delims=" %%V in (`git -C "!tmp_dir!\src" describe --tags --always 2^>nul`) do set "stamp=%%V"
  )
  call :progress_begin "building Wago"
  set "old_cgo=!CGO_ENABLED!"
  set "CGO_ENABLED=0"
  pushd "!tmp_dir!\src" >nul
  go build -trimpath -ldflags "-s -w -X main.version=!stamp!" -o "!tmp_dir!\wago.exe" ./cli/wago >"!tmp_dir!\manager.log" 2>&1
  set "build_status=!ERRORLEVEL!"
  popd >nul
  set "CGO_ENABLED=!old_cgo!"
  if not "!build_status!"=="0" (
    call :progress_fail "Wago build failed"
    type "!tmp_dir!\manager.log" >&2
    call :die "could not build Wago"
    exit /b 1
  )
  call :progress_done "built Wago"
)

if not "!reinstall_mode!"=="minimal" (
  call :progress_begin "cleaning existing Wago installation"
  call :apply_reinstall_cleanup "!reinstall_mode!"
  if errorlevel 1 (
    call :die "reinstall cleanup failed"
    exit /b 1
  )
  call :progress_done "cleaned existing Wago installation (!reinstall_mode!)"
)

call :progress_begin "installing Wago"
if not exist "!bin_dir!" mkdir "!bin_dir!" >nul 2>&1
move /y "!tmp_dir!\wago.exe" "!wago_exe!" >nul
if errorlevel 1 (
  call :die "could not install Wago"
  exit /b 1
)
call :progress_done "installed Wago"

call :progress_begin "saving Wago source"
for %%D in ("!src_dir!\..") do if not exist "%%~fD" mkdir "%%~fD" >nul 2>&1
set "source_backup=!tmp_dir!\source-backup"
if exist "!src_dir!" move /y "!src_dir!" "!source_backup!" >nul
move /y "!tmp_dir!\src" "!src_dir!" >nul
if errorlevel 1 (
  if exist "!source_backup!" move /y "!source_backup!" "!src_dir!" >nul
  call :die "installation is usable, but its source could not be saved"
  exit /b 1
)
call :progress_done "saved Wago source"

call :progress_begin "verifying installation"
call :verify_installation
if errorlevel 1 (
  call :die "the installed Wago command did not start"
  exit /b 1
)
call :progress_done "verified installation"
call :progress_finish "Installed Wago !stamp!"
echo   Command  !wago_exe!
echo.

call :offer_path_setup
if errorlevel 1 (
  echo.
  echo Next step
  echo   Add !bin_dir! to PATH, then run wago.
) else (
  echo.
  setlocal DisableDelayedExpansion
  echo Wago is ready!
  endlocal
  echo Open a new Command Prompt, then run:
  echo   wago version install
)
call :cleanup
exit /b 0

:progress_begin
set "spinner_label=%~1"
set "spinner_stop="
if defined ui_helper if not "%WAGO_NO_TUI%"=="1" (
  set /a spinner_count+=1
  set "spinner_stop=!ui_tmp!\spinner-!spinner_count!.stop"
  if exist "!spinner_stop!" del /q "!spinner_stop!" >nul 2>&1
  if exist "!spinner_stop!.running" del /q "!spinner_stop!.running" >nul 2>&1
  start "" /b "!ui_helper!" spinner "!spinner_stop!" "!spinner_label!"
  call :wait_for_spinner_start
  exit /b 0
)
echo ... !spinner_label!
exit /b 0

:wait_for_spinner_start
for /l %%N in (1,1,50000) do if exist "!spinner_stop!.running" exit /b 0
exit /b 0

:stop_spinner
if not defined spinner_stop exit /b 0
type nul >"!spinner_stop!"
for /l %%N in (1,1,50000) do if not exist "!spinner_stop!.running" goto spinner_stopped
:spinner_stopped
set "spinner_stop="
exit /b 0

:progress_done
call :stop_spinner
if defined ui_helper (
  "!ui_helper!" status done "%~1"
  exit /b 0
)
echo OK  %~1
exit /b 0

:progress_finish
call :stop_spinner
if defined ui_helper (
  "!ui_helper!" status finish "%~1"
  exit /b 0
)
echo OK  %~1
exit /b 0

:progress_fail
call :stop_spinner
if defined ui_helper (
  "!ui_helper!" status fail "%~1"
  exit /b 0
)
echo ERROR  %~1>&2
exit /b 0

:progress_retry
call :stop_spinner
if defined ui_helper (
  "!ui_helper!" status retry "%~1"
  exit /b 0
)
echo -^>  %~1
exit /b 0

:welcome
setlocal DisableDelayedExpansion
echo Welcome to Wago! Let's get you set up.
echo.
endlocal
exit /b 0

:report_install_dir
echo Installing to: !bin_dir!
echo.
exit /b 0

:ensure_ui_helper
if defined ui_helper exit /b 0
if not defined tmp_dir call :make_temp
if errorlevel 1 exit /b 0
set "ui_tmp=!tmp_dir!\ui"
mkdir "!ui_tmp!" >nul 2>&1
if not exist "!ui_tmp!" exit /b 0
chcp 65001 >nul 2>&1
call :manager_target
if errorlevel 1 exit /b 0
set "saved_manager_asset=!manager_asset!"
set "manager_asset=wago-installer-!manager_target!"
call :resolve_manager_release
if errorlevel 1 (
  set "manager_asset=!saved_manager_asset!"
  exit /b 0
)
where curl.exe >nul 2>&1
if errorlevel 1 (
  set "manager_asset=!saved_manager_asset!"
  exit /b 0
)
call :progress_begin "downloading Wago installer !manager_release_tag!"
curl.exe -fsSL "!manager_url!" -o "!tmp_dir!\wago.download" >"!ui_tmp!\download.log" 2>&1
if errorlevel 1 goto ui_download_failed
curl.exe -fsSL "!manager_checksum_url!" -o "!tmp_dir!\manager.sha256" >>"!ui_tmp!\download.log" 2>&1
if errorlevel 1 goto ui_download_failed
call :verify_manager_checksum
if errorlevel 1 goto ui_download_failed
move /y "!tmp_dir!\wago.download" "!ui_tmp!\wago-installer.exe" >nul
if errorlevel 1 goto ui_download_failed
set "ui_helper=!ui_tmp!\wago-installer.exe"
set "manager_asset=!saved_manager_asset!"
call :progress_done "downloaded Wago installer !manager_release_tag!"
exit /b 0

:ui_download_failed
call :progress_retry "installer executable unavailable; using basic prompts"
set "manager_asset=!saved_manager_asset!"
exit /b 0

:ui_select
set "ui_value="
if not defined ui_helper exit /b 1
set "ui_output=!ui_tmp!\selection.txt"
if exist "!ui_output!" del /q "!ui_output!" >nul 2>&1
"!ui_helper!" %~1 "!ui_output!"
if errorlevel 1 exit /b 1
if not exist "!ui_output!" exit /b 1
set /p "ui_value="<"!ui_output!"
if not defined ui_value exit /b 1
exit /b 0

:choose_install_dir
if "!bin_explicit!"=="1" exit /b 0
if not "%WAGO_NO_TUI%"=="1" if not defined WAGO_INSTALL_CHOICE if not defined WAGO_CUSTOM_INSTALL_DIR (
  call :ensure_ui_helper
  if defined ui_helper (
    set "WAGO_UI_BIN_DIR=!bin_dir!"
    set "WAGO_UI_CWD=%CD%"
    call :ui_select install-dir
    set "WAGO_UI_BIN_DIR="
    set "WAGO_UI_CWD="
    if errorlevel 1 exit /b 1
    set "bin_dir=!ui_value!"
    if "!bin_dir:~-1!"=="\" set "bin_dir=!bin_dir:~0,-1!"
    set "wago_exe=!bin_dir!\wago.exe"
    exit /b 0
  )
)
set "install_choice=!WAGO_INSTALL_CHOICE!"
echo Where should Wago be installed?
echo   1. !bin_dir!
echo   2. Custom
if not defined install_choice (
  set /p "install_choice=Select [1]: "
)
if not defined install_choice set "install_choice=1"
if "!install_choice!"=="1" exit /b 0
if not "!install_choice!"=="2" exit /b 1
set "custom_dir=!WAGO_CUSTOM_INSTALL_DIR!"
if not defined custom_dir set /p "custom_dir=Install directory: "
if not defined custom_dir exit /b 1
if "!custom_dir!"=="~" set "custom_dir=%USERPROFILE%"
if "!custom_dir:~0,2!"=="~\" set "custom_dir=%USERPROFILE%\!custom_dir:~2!"
if "!custom_dir:~0,2!"=="~/" set "custom_dir=%USERPROFILE%\!custom_dir:~2!"
if "!custom_dir:~1,1!"==":" (
  set "bin_dir=!custom_dir!"
) else if "!custom_dir:~0,1!"=="\" (
  set "bin_dir=!custom_dir!"
) else (
  set "bin_dir=%CD%\!custom_dir!"
)
if "!bin_dir:~-1!"=="\" set "bin_dir=!bin_dir:~0,-1!"
set "wago_exe=!bin_dir!\wago.exe"
exit /b 0

:choose_reinstall_mode
if not exist "!wago_exe!" (
  set "reinstall_mode=minimal"
  exit /b 0
)
set "reinstall_mode=!WAGO_REINSTALL_MODE!"
if not defined reinstall_mode (
  call :ensure_ui_helper
  set "use_ui=0"
  if defined ui_helper if not "%WAGO_NO_TUI%"=="1" set "use_ui=1"
  if "!use_ui!"=="1" (
    call :ui_select reinstall
    if errorlevel 1 exit /b 1
    set "reinstall_mode=!ui_value!"
    if /i "!reinstall_mode!"=="full" set "reinstall_label=Full"
    if /i "!reinstall_mode!"=="partial" set "reinstall_label=Partial"
    if /i "!reinstall_mode!"=="minimal" set "reinstall_label=Minimal"
    echo Reinstall mode: !reinstall_label!
    echo.
  ) else (
  echo.
  echo Wago is already installed at !wago_exe!.
  echo How should it be reinstalled?
  echo   1. Full     Reset everything, including plugins and settings
  echo   2. Partial  Reset Wago but keep global plugins for reinstall
  echo   3. Minimal  Replace binaries and keep existing state
  set /p "reinstall_choice=Select [3]: "
  if not defined reinstall_choice set "reinstall_choice=3"
  if "!reinstall_choice!"=="1" set "reinstall_mode=full"
  if "!reinstall_choice!"=="2" set "reinstall_mode=partial"
  if "!reinstall_choice!"=="3" set "reinstall_mode=minimal"
  )
)
if /i "!reinstall_mode!"=="full" (
  set "reinstall_mode=full"
  exit /b 0
)
if /i "!reinstall_mode!"=="partial" (
  set "reinstall_mode=partial"
  exit /b 0
)
if /i "!reinstall_mode!"=="minimal" (
  set "reinstall_mode=minimal"
  exit /b 0
)
echo wago: WAGO_REINSTALL_MODE must be full, partial, or minimal>&2
exit /b 1

:go_version_ok
set "go_version="
for /f "usebackq delims=" %%V in (`go env GOVERSION 2^>nul`) do set "go_version=%%V"
if not defined go_version exit /b 1
if /i "!go_version:~0,2!"=="go" set "go_version=!go_version:~2!"
set "go_major=-1"
set "go_minor=-1"
for /f "tokens=1,2 delims=." %%A in ("!go_version!") do (
  set /a "go_major=%%A" >nul 2>&1
  set /a "go_minor=%%B" >nul 2>&1
)
if !go_major! GTR 1 exit /b 0
if !go_major! EQU 1 if !go_minor! GEQ 22 exit /b 0
exit /b 1

:make_temp
set "tmp_dir=%TEMP%\wago-!RANDOM!-!RANDOM!-!RANDOM!"
mkdir "!tmp_dir!" >nul 2>&1
if not exist "!tmp_dir!" exit /b 1
exit /b 0

:manager_target
set "manager_arch=%PROCESSOR_ARCHITECTURE%"
if defined PROCESSOR_ARCHITEW6432 set "manager_arch=%PROCESSOR_ARCHITEW6432%"
if /i "!manager_arch!"=="AMD64" (
  set "manager_target=windows-amd64"
  exit /b 0
)
if /i "!manager_arch!"=="ARM64" (
  set "manager_target=windows-arm64"
  exit /b 0
)
exit /b 1

:clean_release_candidate
set "release_candidate=%~1"
set "release_candidate=!release_candidate: =!"
set "release_candidate=!release_candidate:,=!"
set "release_candidate=!release_candidate:"=!"
exit /b 0

:resolve_manager_release
set "manager_release_tag="
set "manager_override=1"
if /i "!manager_asset:~0,15!"=="wago-installer-" set "manager_override=0"
if "!manager_override!"=="1" if defined WAGO_MANAGER_URL (
  set "manager_release_tag=!version!"
  set "manager_url=!WAGO_MANAGER_URL!"
  set "manager_checksum_url=!manager_url!.sha256"
  if defined WAGO_MANAGER_CHECKSUM_URL set "manager_checksum_url=!WAGO_MANAGER_CHECKSUM_URL!"
  exit /b 0
)
if /i "!version!"=="main" (
  set "release_prefix=canary"
  goto resolve_manager_channel
)
if /i "!version!"=="canary" (
  set "release_prefix=canary"
  goto resolve_manager_channel
)
if /i "!version!"=="nightly" (
  set "release_prefix=nightly"
  goto resolve_manager_channel
)
if /i "!version!"=="latest" goto resolve_manager_latest
if /i "!version:~0,1!"=="v" goto resolve_manager_exact
if /i "!version:~0,7!"=="canary-" goto resolve_manager_exact
if /i "!version:~0,8!"=="nightly-" goto resolve_manager_exact
exit /b 1

:resolve_manager_channel
where curl.exe >nul 2>&1
if errorlevel 1 exit /b 1
curl.exe -fsSL "!release_api!?per_page=100" -o "!tmp_dir!\releases.json" >nul 2>&1
if errorlevel 1 exit /b 1
for /f "usebackq tokens=2 delims=:" %%A in (`findstr /c:"tag_name" "!tmp_dir!\releases.json"`) do if not defined manager_release_tag (
  call :clean_release_candidate "%%A"
  if /i "!release_prefix!"=="canary" if /i "!release_candidate:~0,7!"=="canary-" set "manager_release_tag=!release_candidate!"
  if /i "!release_prefix!"=="nightly" if /i "!release_candidate:~0,8!"=="nightly-" set "manager_release_tag=!release_candidate!"
)
if not defined manager_release_tag exit /b 1
goto resolve_manager_urls

:resolve_manager_latest
where curl.exe >nul 2>&1
if errorlevel 1 exit /b 1
curl.exe -fsSL "!release_api!/latest" -o "!tmp_dir!\release.json" >nul 2>&1
if errorlevel 1 exit /b 1
for /f "usebackq tokens=2 delims=:" %%A in (`findstr /c:"tag_name" "!tmp_dir!\release.json"`) do if not defined manager_release_tag (
  call :clean_release_candidate "%%A"
  set "manager_release_tag=!release_candidate!"
)
if not defined manager_release_tag exit /b 1
goto resolve_manager_urls

:resolve_manager_exact
set "manager_release_tag=!version!"

:resolve_manager_urls
set "manager_url=!release_download_base!/download/!manager_release_tag!/!manager_asset!"
set "manager_checksum_url=!manager_url!.sha256"
if "!manager_override!"=="1" if defined WAGO_MANAGER_CHECKSUM_URL set "manager_checksum_url=!WAGO_MANAGER_CHECKSUM_URL!"
exit /b 0

:verify_manager_checksum
set "expected_hash="
for /f "usebackq tokens=1" %%H in ("!tmp_dir!\manager.sha256") do if not defined expected_hash set "expected_hash=%%H"
if not defined expected_hash exit /b 1
if "!expected_hash:~63,1!"=="" exit /b 1
if not "!expected_hash:~64,1!"=="" exit /b 1
where certutil.exe >nul 2>&1
if errorlevel 1 exit /b 1
certutil.exe -hashfile "!tmp_dir!\wago.download" SHA256 >"!tmp_dir!\manager.hash" 2>&1
if errorlevel 1 exit /b 1
set "actual_hash="
for /f "usebackq skip=1 tokens=*" %%H in ("!tmp_dir!\manager.hash") do if not defined actual_hash set "actual_hash=%%H"
set "actual_hash=!actual_hash: =!"
if /i not "!actual_hash!"=="!expected_hash!" exit /b 1
exit /b 0

:download_manager_release
call :manager_target
if errorlevel 1 exit /b 1
set "manager_asset=wago-!manager_target!"
call :resolve_manager_release
if errorlevel 1 (
  call :progress_retry "release manager unavailable; building from source"
  exit /b 1
)
where curl.exe >nul 2>&1
if errorlevel 1 (
  call :progress_retry "release manager unavailable; building from source"
  exit /b 1
)
call :progress_begin "downloading Wago manager !manager_release_tag!"
curl.exe -fsSL "!manager_url!" -o "!tmp_dir!\wago.download" >"!tmp_dir!\manager-download.log" 2>&1
if errorlevel 1 goto manager_download_failed
curl.exe -fsSL "!manager_checksum_url!" -o "!tmp_dir!\manager.sha256" >>"!tmp_dir!\manager-download.log" 2>&1
if errorlevel 1 goto manager_download_failed
call :verify_manager_checksum
if errorlevel 1 goto manager_download_failed
move /y "!tmp_dir!\wago.download" "!tmp_dir!\wago.exe" >nul
if errorlevel 1 goto manager_download_failed
set "manager_method=release"
call :progress_done "downloaded Wago manager !manager_release_tag!"
exit /b 0

:manager_download_failed
call :progress_retry "release manager unavailable; building from source"
if exist "!tmp_dir!\manager-download.log" type "!tmp_dir!\manager-download.log" >&2
exit /b 1

:fetch_source
set "source_method="
call :progress_begin "fetching Wago source with git"
where git.exe >nul 2>&1
if not errorlevel 1 (
  git clone --depth 1 --branch "!source_version!" "!repo_url!" "!tmp_dir!\src" >"!tmp_dir!\git.log" 2>&1
  if not errorlevel 1 (
    set "source_method=git"
    call :progress_done "fetched Wago source with git"
    exit /b 0
  )
  if exist "!tmp_dir!\src" rmdir /s /q "!tmp_dir!\src"
  git clone "!repo_url!" "!tmp_dir!\src" >>"!tmp_dir!\git.log" 2>&1
  if not errorlevel 1 (
    git -C "!tmp_dir!\src" checkout --quiet "!source_version!" >>"!tmp_dir!\git.log" 2>&1
    if not errorlevel 1 (
      set "source_method=git"
      call :progress_done "fetched Wago source with git"
      exit /b 0
    )
  )
  if exist "!tmp_dir!\src" rmdir /s /q "!tmp_dir!\src"
)
call :progress_retry "git fetch failed; trying source archive"

where curl.exe >nul 2>&1
if errorlevel 1 goto fetch_failed
where tar.exe >nul 2>&1
if errorlevel 1 goto fetch_failed
call :progress_begin "downloading Wago source archive"
curl.exe -fsSL "!source_archive_url!" -o "!tmp_dir!\wago-source.zip" >"!tmp_dir!\archive.log" 2>&1
if errorlevel 1 goto fetch_failed
mkdir "!tmp_dir!\archive" >nul 2>&1
tar.exe -xf "!tmp_dir!\wago-source.zip" -C "!tmp_dir!\archive" >>"!tmp_dir!\archive.log" 2>&1
if errorlevel 1 goto fetch_failed
set "archive_source="
set "archive_count=0"
for /d %%D in ("!tmp_dir!\archive\*") do (
  set /a archive_count+=1
  set "archive_source=%%~fD"
)
if not "!archive_count!"=="1" goto fetch_failed
if not exist "!archive_source!\go.mod" goto fetch_failed
move /y "!archive_source!" "!tmp_dir!\src" >nul
if errorlevel 1 goto fetch_failed
set "source_method=archive"
call :progress_done "downloaded and unpacked Wago source archive"
exit /b 0

:fetch_failed
call :progress_fail "source fetch failed"
if exist "!tmp_dir!\git.log" type "!tmp_dir!\git.log" >&2
if exist "!tmp_dir!\archive.log" type "!tmp_dir!\archive.log" >&2
exit /b 1

:verify_installation
set "verify_timeout=10"
if defined WAGO_VERIFY_TIMEOUT set "verify_timeout=%WAGO_VERIFY_TIMEOUT%"
if defined ui_helper (
  "!ui_helper!" verify "!wago_exe!" "!verify_timeout!" >nul 2>&1
  exit /b !ERRORLEVEL!
)
"!wago_exe!" self --help >nul 2>&1
exit /b !ERRORLEVEL!

:apply_reinstall_cleanup
set "cleanup_mode=%~1"
if "!cleanup_mode!"=="minimal" exit /b 0
if "!cleanup_mode!"=="partial" (
  call :safe_remove "!wago_data!\versions" || exit /b 1
  call :safe_remove "!wago_config!" || exit /b 1
  call :safe_remove "!wago_cache!" || exit /b 1
  call :safe_remove "!src_dir!" || exit /b 1
  if exist "!wago_exe!" del /q "!wago_exe!" >nul 2>&1
  exit /b 0
)
if "!cleanup_mode!"=="full" (
  call :safe_remove "!wago_data!" || exit /b 1
  call :safe_remove "!wago_config!" || exit /b 1
  call :safe_remove "!wago_cache!" || exit /b 1
  call :safe_remove "!src_dir!" || exit /b 1
  call :safe_remove "!default_wago_root!" || exit /b 1
  if defined WAGO_HOME (
    call :safe_remove "!wago_root!" || exit /b 1
  )
  if exist "!wago_exe!" del /q "!wago_exe!" >nul 2>&1
  exit /b 0
)
exit /b 1

:safe_remove
set "remove_target=%~f1"
if not defined remove_target exit /b 1
if /i "!remove_target!"=="%USERPROFILE%" exit /b 1
if /i "!remove_target!"=="%CD%" exit /b 1
if "!remove_target:~3!"=="" exit /b 1
if exist "!remove_target!" rmdir /s /q "!remove_target!"
if exist "!remove_target!" exit /b 1
exit /b 0

:offer_path_setup
if "%WAGO_NO_MODIFY_PATH%"=="1" exit /b 1
set "path_choice=!WAGO_PATH_SETUP!"
if not defined path_choice (
  call :ensure_ui_helper
  set "use_ui=0"
  if defined ui_helper if not "%WAGO_NO_TUI%"=="1" set "use_ui=1"
  if "!use_ui!"=="1" (
    call :ui_select path
    if errorlevel 1 exit /b 1
    set "path_choice=!ui_value!"
  ) else (
    echo Add Wago to your user PATH?
    set /p "path_choice=Select [Y/n]: "
  )
)
if not defined path_choice set "path_choice=yes"
if /i "!path_choice!"=="n" exit /b 1
if /i "!path_choice!"=="no" exit /b 1
if /i "!path_choice!"=="none" (
  echo PATH setup: skipped
  echo.
  exit /b 1
)

set "user_path="
set "user_path_type=REG_EXPAND_SZ"
if defined WAGO_TEST_USER_PATH (
  set "user_path=!WAGO_TEST_USER_PATH!"
) else (
  for /f "tokens=2,*" %%A in ('reg.exe query "HKCU\Environment" /v Path 2^>nul ^| findstr /i /r "[ ]Path[ ]"') do (
    set "user_path_type=%%A"
    set "user_path=%%B"
  )
)
set "path_already_configured=0"
for %%P in ("!user_path:;=";"!") do (
  if /i "%%~P"=="!bin_dir!" set "path_already_configured=1"
)
if "!path_already_configured!"=="1" (
  echo OK  PATH already configured
  set "PATH=!bin_dir!;!PATH!"
  exit /b 0
)
if defined user_path (
  set "new_user_path=!bin_dir!;!user_path!"
) else (
  set "new_user_path=!bin_dir!"
)
if defined WAGO_TEST_USER_PATH (
  echo path=!new_user_path!
) else (
  reg.exe add "HKCU\Environment" /v Path /t !user_path_type! /d "!new_user_path!" /f >nul
  if errorlevel 1 exit /b 1
)
set "PATH=!bin_dir!;!PATH!"
echo OK  Added Wago to PATH
exit /b 0

:cleanup
call :stop_spinner
if defined tmp_dir if exist "!tmp_dir!" rmdir /s /q "!tmp_dir!"
if defined ui_tmp if exist "!ui_tmp!" rmdir /s /q "!ui_tmp!"
exit /b 0

:die
call :stop_spinner
echo wago: %~1>&2
call :cleanup
exit /b 1

:install_dir_only
call :welcome
call :choose_install_dir
if errorlevel 1 (
  echo Cancelled.
  call :cleanup
  exit /b 0
)
call :report_install_dir
echo bin=!bin_dir!
call :cleanup
exit /b 0

:reinstall_check_only
call :choose_reinstall_mode
if errorlevel 1 (
  echo Cancelled.
  call :cleanup
  exit /b 0
)
if "!reinstall_mode!"=="full" echo mode=full plugins=removed
if "!reinstall_mode!"=="partial" echo mode=partial plugins=preserved
if "!reinstall_mode!"=="minimal" echo mode=minimal state=preserved
call :cleanup
exit /b 0

:reinstall_cleanup_only
call :apply_reinstall_cleanup "%WAGO_INTERNAL_REINSTALL_CLEANUP_ONLY%"
if errorlevel 1 exit /b 1
if "%WAGO_INTERNAL_REINSTALL_CLEANUP_ONLY%"=="full" echo mode=full plugins=removed
if "%WAGO_INTERNAL_REINSTALL_CLEANUP_ONLY%"=="partial" echo mode=partial plugins=preserved
if "%WAGO_INTERNAL_REINSTALL_CLEANUP_ONLY%"=="minimal" echo mode=minimal state=preserved
call :cleanup
exit /b 0

:path_setup_only
call :offer_path_setup
if errorlevel 1 echo Add !bin_dir! to PATH to use wago.
call :cleanup
exit /b 0

:verify_only
call :verify_installation
set "verify_status=!ERRORLEVEL!"
call :cleanup
exit /b !verify_status!

:fetch_only
call :make_temp
if errorlevel 1 exit /b 1
call :fetch_source
set "fetch_status=!ERRORLEVEL!"
if "!fetch_status!"=="0" echo source=!source_method!
call :cleanup
exit /b !fetch_status!

:manager_only
call :make_temp
if errorlevel 1 exit /b 1
set "manager_method=source"
call :download_manager_release
set "manager_status=!ERRORLEVEL!"
if "!manager_status!"=="0" echo manager=!manager_method! tag=!manager_release_tag!
call :cleanup
exit /b !manager_status!

:installer_only
call :make_temp
if errorlevel 1 exit /b 1
call :ensure_ui_helper
if not defined ui_helper (
  call :cleanup
  exit /b 1
)
echo installer=release tag=!manager_release_tag!
call :cleanup
exit /b 0
