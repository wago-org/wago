param(
    [string]$Version = "1.0.41",
    [string]$SHA256 = "37285ec7244384ffd382841f93fd23335aae846c92016a132d765c60f27a2f31"
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$archiveName = "wabt-$Version-windows-x64.tar.gz"
$archivePath = Join-Path $env:RUNNER_TEMP $archiveName
$extractRoot = Join-Path $env:RUNNER_TEMP "wabt-windows"
$url = "https://github.com/WebAssembly/wabt/releases/download/$Version/$archiveName"

Invoke-WebRequest -Uri $url -OutFile $archivePath
$actualSHA256 = (Get-FileHash -Algorithm SHA256 $archivePath).Hash.ToLowerInvariant()
if ($actualSHA256 -ne $SHA256) {
    throw "WABT checksum mismatch: expected $SHA256, got $actualSHA256"
}

New-Item -ItemType Directory -Path $extractRoot -Force | Out-Null
& tar.exe -xzf $archivePath -C $extractRoot
if ($LASTEXITCODE -ne 0) {
    throw "failed to extract $archiveName"
}

$binPath = Join-Path $extractRoot "wabt-$Version/bin"
$wat2wasm = Join-Path $binPath "wat2wasm.exe"
if (-not (Test-Path -PathType Leaf $wat2wasm)) {
    throw "WABT archive does not contain $wat2wasm"
}

Add-Content -Path $env:GITHUB_PATH -Value $binPath -Encoding utf8
& $wat2wasm --version
if ($LASTEXITCODE -ne 0) {
    throw "wat2wasm failed to start"
}
