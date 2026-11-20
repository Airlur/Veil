# deploy-uiaccess.ps1 - install the UIAccess build so Windows will grant it the UIAccess token.
# Needs admin (one UAC prompt). Run build-uiaccess.ps1 first.
#
#   1. trusts build\uiaccess\VeilDev.cer in LocalMachine\Root   (UIAccess requires a trusted signature)
#   2. copies Veil.exe to C:\Program Files\Veil\                  (UIAccess requires a secure location)
#   3. starts it through Explorer (UIAccess exes cannot be started with plain CreateProcess)
#
# Undo with:  deploy-uiaccess.ps1 -Uninstall
# (ASCII on purpose, see build-uiaccess.ps1.)

param([switch]$Uninstall, [switch]$NoStart)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$out  = Join-Path $root 'build\uiaccess'
$dest = Join-Path $env:ProgramFiles 'Veil'
$subject = 'CN=Veil Dev Code Signing'

$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
    ).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) {
    $argList = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "`"$PSCommandPath`"")
    if ($Uninstall) { $argList += '-Uninstall' }
    if ($NoStart)   { $argList += '-NoStart' }
    Start-Process powershell -Verb RunAs -ArgumentList $argList -Wait
    return
}

try {
    # A running copy locks the exe; ask every instance to exit.
    Get-Process Veil -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Milliseconds 500

    if ($Uninstall) {
        Get-ChildItem Cert:\LocalMachine\Root | Where-Object { $_.Subject -eq $subject } | Remove-Item
        if (Test-Path $dest) { Remove-Item $dest -Recurse -Force }
        Write-Host 'Uninstalled: cert removed from LocalMachine\Root, Program Files\Veil deleted.'
    } else {
        $exe = Join-Path $out 'Veil.exe'
        $cer = Join-Path $out 'VeilDev.cer'
        if (-not (Test-Path $exe) -or -not (Test-Path $cer)) { throw 'Run scripts\build-uiaccess.ps1 first.' }

        Import-Certificate -FilePath $cer -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
        New-Item -ItemType Directory -Force -Path $dest | Out-Null
        Copy-Item $exe $dest -Force

        $sig = Get-AuthenticodeSignature (Join-Path $dest 'Veil.exe')
        Write-Host "Installed to $dest (signature: $($sig.Status))"
        if ($sig.Status -ne 'Valid') { throw 'Signature is not valid after trusting the cert.' }

        # Explorer starts it unelevated and through ShellExecute, which is what UIAccess needs.
        if (-not $NoStart) { Start-Process explorer.exe (Join-Path $dest 'Veil.exe') }
    }
} catch {
    Write-Host "FAILED: $_" -ForegroundColor Red
}
Read-Host 'Press Enter to close'
