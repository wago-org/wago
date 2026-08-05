$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$baseURL = if ($env:WAGO_INSTALL_BASE_URL) {
    $env:WAGO_INSTALL_BASE_URL.TrimEnd("/")
} else {
    "https://install.wago.sh"
}
$temporaryDirectory = [IO.Path]::GetTempPath()
$loader = Join-Path $temporaryDirectory ("wago-install-{0}.cmd" -f [guid]::NewGuid().ToString("N"))
$refreshRequest = Join-Path $temporaryDirectory ("wago-refresh-{0}.request" -f [guid]::NewGuid().ToString("N"))
$previousRefreshRequest = $env:WAGO_PATH_REFRESH_FILE
$previousRefreshChoice = $env:WAGO_REFRESH_PATH
$runsInChildShell = [Environment]::GetCommandLineArgs()[-1] -eq "-"

try {
    Invoke-WebRequest "$baseURL/install.cmd" -OutFile $loader -UseBasicParsing
    $env:WAGO_PATH_REFRESH_FILE = $refreshRequest
    if ($runsInChildShell) {
        $env:WAGO_REFRESH_PATH = "no"
    }

    $commandProcessor = if ($env:ComSpec) {
        $env:ComSpec
    } else {
        Join-Path $env:SystemRoot "System32\cmd.exe"
    }
    & $commandProcessor /d /q /c "call `"$loader`""
    $installerStatus = $LASTEXITCODE

    if ($installerStatus -eq 0 -and (Test-Path -LiteralPath $refreshRequest)) {
        [string[]] $paths = @(
            [Environment]::GetEnvironmentVariable("Path", "Machine")
            [Environment]::GetEnvironmentVariable("Path", "User")
        ) | Where-Object { $_ }
        if ($paths.Count -gt 0) {
            $env:Path = [string]::Join(";", $paths)
        }
    }
} finally {
    if ($null -eq $previousRefreshRequest) {
        Remove-Item Env:WAGO_PATH_REFRESH_FILE -ErrorAction SilentlyContinue
    } else {
        $env:WAGO_PATH_REFRESH_FILE = $previousRefreshRequest
    }
    if ($null -eq $previousRefreshChoice) {
        Remove-Item Env:WAGO_REFRESH_PATH -ErrorAction SilentlyContinue
    } else {
        $env:WAGO_REFRESH_PATH = $previousRefreshChoice
    }
    Remove-Item -LiteralPath $loader, $refreshRequest -Force -ErrorAction SilentlyContinue
}

if ($installerStatus -ne 0) {
    exit $installerStatus
}
