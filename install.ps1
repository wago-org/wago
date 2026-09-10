$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

function Get-WagoArchitecture {
    try {
        $operatingSystemArchitecture = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
        if ($operatingSystemArchitecture -eq "Arm64") {
            return "arm64"
        }
        if ($operatingSystemArchitecture -eq "X64") {
            return "amd64"
        }
    } catch {
        # Fall back for older Windows PowerShell runtimes.
    }
    $architectures = @($env:PROCESSOR_ARCHITEW6432, $env:PROCESSOR_ARCHITECTURE)
    if ($architectures -contains "ARM64") {
        return "arm64"
    }
    if ($architectures -contains "AMD64") {
        return "amd64"
    }
    throw "wago: this Windows architecture is not supported"
}

function Get-WagoLatestTag {
    $release = Invoke-RestMethod "$releaseAPI/latest" -UseBasicParsing
    $tag = [string]$release.tag_name
    if ($tag -notmatch '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$') {
        throw "wago: the latest release has an invalid tag"
    }
    return $tag
}

function Get-WagoBetaTag {
    for ($page = 1; $page -le 10; $page++) {
        $releases = @(Invoke-RestMethod "$releaseAPI`?per_page=100&page=$page" -UseBasicParsing)
        $beta = $releases |
            Where-Object {
                -not $_.draft -and
                ([string]$_.tag_name) -match '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-beta\.(0|[1-9][0-9]*)$'
            } |
            Sort-Object { [DateTimeOffset]$_.published_at } -Descending |
            Select-Object -First 1
        if ($null -ne $beta) {
            return [string]$beta.tag_name
        }
        if ($releases.Count -lt 100) {
            break
        }
    }
    return $null
}

function Get-WagoDownloadTags([string]$version) {
    if ($version -eq "latest") {
        return @(Get-WagoLatestTag)
    }
    if ($version -eq "main") {
        $tags = @()
        try {
            $tags += Get-WagoLatestTag
        } catch {
            # A beta release can carry the installer when no stable release exists.
        }
        $beta = Get-WagoBetaTag
        if ($beta -and $tags -notcontains $beta) {
            $tags += $beta
        }
        return $tags
    }
    if ($version -eq "beta") {
        return @(Get-WagoBetaTag)
    }
    if ($version -eq "canary" -or $version -match '^canary@[0-9a-fA-F]{40}$' -or $version -match '^v[0-9]+\.[0-9]+\.[0-9]+-canary\.g[0-9a-fA-F]{7}$') {
        return @(Get-WagoBetaTag)
    }
    if ($version -match '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.(0|[1-9][0-9]*))?$') {
        return @($version)
    }
    return @()
}

$version = if ($env:WAGO_VERSION) { $env:WAGO_VERSION } else { "main" }
$releaseRepo = if ($env:WAGO_RELEASE_REPO) { $env:WAGO_RELEASE_REPO } else { "wago-org/wago" }
$releaseAPI = if ($env:WAGO_RELEASES_API_URL) {
    $env:WAGO_RELEASES_API_URL.TrimEnd("/")
} else {
    "https://api.github.com/repos/$releaseRepo/releases"
}
$releaseDownloadBase = if ($env:WAGO_RELEASE_DOWNLOAD_BASE) {
    $env:WAGO_RELEASE_DOWNLOAD_BASE.TrimEnd("/")
} else {
    "https://github.com/$releaseRepo/releases"
}

$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("wago-install-{0}" -f [guid]::NewGuid().ToString("N"))
$refreshRequest = Join-Path ([IO.Path]::GetTempPath()) ("wago-refresh-{0}.request" -f [guid]::NewGuid().ToString("N"))
$previousVersion = $env:WAGO_VERSION
$previousRefreshRequest = $env:WAGO_PATH_REFRESH_FILE
$previousRefreshChoice = $env:WAGO_REFRESH_PATH
$runsInChildShell = [Environment]::GetCommandLineArgs()[-1] -eq "-"
$installerStatus = 1

try {
    $env:WAGO_PATH_REFRESH_FILE = $refreshRequest
    if ($runsInChildShell) {
        $env:WAGO_REFRESH_PATH = "no"
    }

    if ($env:WAGO_INSTALLER) {
        if (-not (Test-Path -LiteralPath $env:WAGO_INSTALLER -PathType Leaf)) {
            throw "wago: WAGO_INSTALLER does not exist: $env:WAGO_INSTALLER"
        }
        $installer = $env:WAGO_INSTALLER
    } else {
        [void][IO.Directory]::CreateDirectory($temporaryDirectory)
        $architecture = Get-WagoArchitecture
        $asset = "wago-installer-windows-$architecture"
        $installer = Join-Path $temporaryDirectory "installer.exe"
        $checksum = Join-Path $temporaryDirectory "installer.sha256"
        $downloaded = $false
        $downloadError = $null

        foreach ($tag in @(Get-WagoDownloadTags $version)) {
            if (-not $tag) {
                continue
            }
            $url = "$releaseDownloadBase/download/$tag/$asset"
            try {
                Invoke-WebRequest $url -OutFile $installer -UseBasicParsing
                Invoke-WebRequest "$url.sha256" -OutFile $checksum -UseBasicParsing
            } catch {
                $downloadError = $_.Exception.Message
                Remove-Item -LiteralPath $installer, $checksum -Force -ErrorAction SilentlyContinue
                continue
            }

            $expectedHash = ((Get-Content -LiteralPath $checksum -Raw) -split '\s+')[0]
            $actualHash = (Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash
            if ($expectedHash -notmatch '^[0-9a-fA-F]{64}$' -or $actualHash -ne $expectedHash) {
                throw "wago: the downloaded installer could not be verified; try again when the release service is available"
            }
            $downloaded = $true
            break
        }

        if (-not $downloaded) {
            if ($env:WAGO_INSTALLER_DEBUG -and $downloadError) {
                throw "wago: the installer is unavailable: $downloadError"
            }
            throw "wago: the installer is unavailable; check your internet connection and try again"
        }
    }

    $env:WAGO_VERSION = $version
    & $installer install @args
    $installerStatus = $LASTEXITCODE
    if ($installerStatus -eq 2) {
        throw "wago: this installer release predates the native install flow; wait for the channel to update and try again"
    }
    if ($installerStatus -ne 0) {
        throw "wago: the installer exited with status $installerStatus"
    }

    if (Test-Path -LiteralPath $refreshRequest) {
        [string[]]$paths = @(
            [Environment]::GetEnvironmentVariable("Path", "Machine")
            [Environment]::GetEnvironmentVariable("Path", "User")
        ) | Where-Object { $_ }
        if ($paths.Count -gt 0) {
            $env:Path = [string]::Join(";", $paths)
        }
    }
} catch {
    $installerStatus = 1
    $message = $_.Exception.Message
    if (-not $message.StartsWith("wago:")) {
        $message = "wago: the installer is unavailable; check your internet connection and try again"
    }
    [Console]::Error.WriteLine($message)
} finally {
    if ($null -eq $previousVersion) {
        Remove-Item Env:WAGO_VERSION -ErrorAction SilentlyContinue
    } else {
        $env:WAGO_VERSION = $previousVersion
    }
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
    foreach ($path in @($temporaryDirectory, $refreshRequest)) {
        if (Test-Path -LiteralPath $path) {
            Remove-Item -LiteralPath $path -Recurse -Force
        }
    }
}

if ($installerStatus -ne 0) {
    exit $installerStatus
}
