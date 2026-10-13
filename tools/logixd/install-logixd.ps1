<#
.SYNOPSIS
    Download logixd from a nautilus release and install it, in one command.

.DESCRIPTION
    logixd is published as source, not as a binary: it links the Logix
    Designer SDK's .NET client, which is Rockwell's and comes only with the
    SDK install. So it is built here, on the licensed machine, and nothing of
    Rockwell's ever leaves it.

    This script fetches logixd.zip from a GitHub release of nautilus,
    installs a private .NET 10 SDK if the machine has none, and hands over to
    install.ps1 (preflight, build, token, scheduled task, firewall rule,
    probe). Run it in an ELEVATED PowerShell on the machine with Studio 5000
    and the Logix Designer SDK:

        irm https://github.com/joyautomation/nautilus/releases/latest/download/install-logixd.ps1 | iex

    With options (any install.ps1 parameter passes through):

        & ([scriptblock]::Create((irm https://github.com/joyautomation/nautilus/releases/latest/download/install-logixd.ps1))) -Listen 0.0.0.0
        & ([scriptblock]::Create((irm .../install-logixd.ps1))) -Version 0.16.2
        & ([scriptblock]::Create((irm .../install-logixd.ps1))) -Uninstall

    Re-running it upgrades. The source is kept in %ProgramData%\logixd\src.
#>
[CmdletBinding()]
param(
    # A nautilus release ("0.16.2" or "v0.16.2"); default the latest.
    [string] $Version = 'latest',
    # A logixd.zip to use instead of downloading one (a path or a URL).
    [string] $Zip = '',
    # Install a private .NET 10 SDK even when one is already on the machine.
    [switch] $InstallDotnet,
    # Where a private .NET goes.
    [string] $DotnetDir = "$env:ProgramFiles\logixd-dotnet",
    # Everything else goes to install.ps1 (-Listen, -Port, -TaskName, -Uninstall, ...).
    [Parameter(ValueFromRemainingArguments = $true)] $Rest
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest is many times slower with it
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

function Ok  ($m) { Write-Host "  [ok]   $m" -ForegroundColor Green }
function Die ($m) { Write-Host "  [fail] $m" -ForegroundColor Red; throw "install-logixd: $m" }

$id = [Security.Principal.WindowsIdentity]::GetCurrent()
if (-not (New-Object Security.Principal.WindowsPrincipal $id).IsInRole(
          [Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Die "run this in an ELEVATED PowerShell -- install.ps1 registers a scheduled task and a firewall rule"
}

Write-Host "`nFetching logixd`n"
$repo = 'https://github.com/joyautomation/nautilus'
if (-not $Zip) {
    if ($Version -eq 'latest') { $Zip = "$repo/releases/latest/download/logixd.zip" }
    else { $Zip = "$repo/releases/download/v$($Version.TrimStart('v'))/logixd.zip" }
}
$root = Join-Path $env:ProgramData 'logixd'
$src  = Join-Path $root 'src'
$tmp  = Join-Path $env:TEMP "logixd-$([guid]::NewGuid().ToString('N')).zip"
if ($Zip -match '^https?://') {
    try { Invoke-WebRequest -UseBasicParsing -Uri $Zip -OutFile $tmp }
    catch { Die "could not download $Zip ($($_.Exception.Message)). Is -Version a nautilus release that carries logixd.zip (v0.16.2 or later)?" }
} else {
    if (-not (Test-Path $Zip)) { Die "no such file: $Zip" }
    Copy-Item $Zip $tmp
}
# A fresh tree each time, so a file dropped by a newer release never lingers.
New-Item -ItemType Directory -Force -Path $root | Out-Null
if (Test-Path $src) { Remove-Item $src -Recurse -Force }
Expand-Archive -Path $tmp -DestinationPath $src
Remove-Item $tmp -Force
if (-not (Test-Path (Join-Path $src 'install.ps1'))) { Die "$Zip has no install.ps1 at its root" }
$ver = if (Test-Path (Join-Path $src 'VERSION')) { (Get-Content (Join-Path $src 'VERSION') -Raw).Trim() } else { 'unversioned' }
Ok "logixd $ver source in $src"

# install.ps1 needs the .NET 10 SDK to build. Look where it looks; install a
# private one only when there is none (or when asked).
# The pass-through arguments, as names and values: an array splatted into a
# script binds "-Listen" as a positional string, not as the parameter.
$pass = @{}
for ($i = 0; $i -lt @($Rest).Count; $i++) {
    if ("$($Rest[$i])" -notmatch '^-([A-Za-z]+)$') { Die "unexpected argument '$($Rest[$i])'" }
    $name = $Matches[1]
    if ($i + 1 -lt @($Rest).Count -and "$($Rest[$i + 1])" -notmatch '^-[A-Za-z]') { $pass[$name] = $Rest[$i + 1]; $i++ }
    else { $pass[$name] = $true }
}
if (-not $pass.ContainsKey('Uninstall')) {
    $have = $null
    if (-not $InstallDotnet) {
        $cands = @([Environment]::GetEnvironmentVariable('DOTNET_ROOT_X64','Machine'), $DotnetDir,
                   'C:\dotnet10', "$env:ProgramFiles\dotnet") | Where-Object { $_ }
        foreach ($c in $cands) {
            $exe = Join-Path $c 'dotnet.exe'
            if ((Test-Path $exe) -and ((& $exe --list-sdks 2>$null) -match '^10\.')) { $have = $c; break }
        }
    }
    if ($have) {
        Ok ".NET 10 SDK at $have"
    } else {
        Write-Host "  installing a private .NET 10 SDK (x64) into $DotnetDir ..."
        $di = Join-Path $env:TEMP 'dotnet-install.ps1'
        Invoke-WebRequest -UseBasicParsing -Uri 'https://dot.net/v1/dotnet-install.ps1' -OutFile $di
        # -NoPath: never touch PATH or DOTNET_ROOT. A machine-wide DOTNET_ROOT
        # pointing at x64 is exactly what breaks FactoryTalk auth.
        & $di -Channel 10.0 -Architecture x64 -InstallDir $DotnetDir -NoPath | Out-Null
        Remove-Item $di -Force
        $have = $DotnetDir
        Ok ".NET 10 SDK installed at $have (not on PATH; used only by logixd)"
    }
    $pass['DotnetRoot'] = $have
}

& (Join-Path $src 'install.ps1') @pass
