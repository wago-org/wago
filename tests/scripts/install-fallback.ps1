param(
    [string]$InstallerScript = (Join-Path $PSScriptRoot "../../install.ps1"),
    [int]$BenchmarkIterations = 0
)
$ErrorActionPreference = "Stop"
# Load only release selection. Do not execute the installer or make requests.
$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile(
    (Resolve-Path -LiteralPath $InstallerScript).Path, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -ne 0) { throw "Installer script has parse errors" }
$definition = $ast.Find({ param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
    $node.Name -eq "Get-WagoDownloadTags"
}, $true)
if ($null -eq $definition) { throw "Release selection function is missing" }
Invoke-Expression $definition.Extent.Text

$script:latestFails = $false
$script:betaFails = $false
$script:betaTag = "v1.2.0-beta.1"
function Get-WagoLatestTag {
    if ($script:latestFails) { throw "latest lookup failed" }
    return "v1.1.0"
}
function Get-WagoBetaTag {
    if ($script:betaFails) { throw "beta lookup failed" }
    return $script:betaTag
}
function Assert-Tags([string]$selector, [string[]]$expected) {
    $actual = @(Get-WagoDownloadTags $selector)
    if ($actual.Count -ne $expected.Count -or ($actual -join "|") -ne ($expected -join "|")) {
        throw "Selector $selector returned $actual; expected $expected"
    }
}
function Assert-Failure([string]$selector, [string]$expected) {
    $failed = $false
    try { $null = Get-WagoDownloadTags $selector } catch {
        $failed = $true
        if ($_.Exception.Message -ne $expected) {
            throw "Selector $selector failed with $_; expected $expected"
        }
    }
    if (-not $failed) { throw "Selector $selector should fail" }
}

if ($BenchmarkIterations -gt 0) {
    # Measure selection only, with identical local stubs before and after.
    for ($i = 0; $i -lt 1000; $i++) { $null = Get-WagoDownloadTags "main" }
    $watch = [Diagnostics.Stopwatch]::new()
    $before = [GC]::GetAllocatedBytesForCurrentThread()
    $watch.Start()
    for ($i = 0; $i -lt $BenchmarkIterations; $i++) { $null = Get-WagoDownloadTags "main" }
    $watch.Stop()
    $allocated = [GC]::GetAllocatedBytesForCurrentThread() - $before
    [pscustomobject]@{
        Iterations = $BenchmarkIterations
        NanosecondsPerSelection = $watch.Elapsed.TotalMilliseconds * 1000000 / $BenchmarkIterations
        AllocatedBytesPerSelection = $allocated / $BenchmarkIterations
    } | ConvertTo-Json -Compress
    exit 0
}

Assert-Tags "main" @("v1.1.0", "v1.2.0-beta.1")
$script:betaFails = $true
Assert-Tags "main" @("v1.1.0")
Assert-Tags "latest" @("v1.1.0")
Assert-Failure "beta" "beta lookup failed"
Assert-Failure "canary" "beta lookup failed"
$script:latestFails = $true
Assert-Failure "main" "beta lookup failed"
Assert-Failure "latest" "latest lookup failed"
Assert-Tags "v1.1.0" @("v1.1.0")
Assert-Tags "v1.2.0-beta.1" @("v1.2.0-beta.1")
Assert-Tags "invalid" @()
$script:betaFails = $false
Assert-Tags "main" @("v1.2.0-beta.1")
Assert-Tags "beta" @("v1.2.0-beta.1")
Assert-Tags "canary" @("v1.2.0-beta.1")
$script:betaTag = $null
Assert-Tags "main" @()
$script:latestFails = $false
Assert-Tags "main" @("v1.1.0")
$script:betaTag = "v1.1.0"
Assert-Tags "main" @("v1.1.0")
Assert-Tags "latest" @("v1.1.0")
Write-Output "Installer release selection tests passed"
