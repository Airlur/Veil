# build-uiaccess.ps1 - build the UIAccess variant of Veil and sign it with a local self-signed cert.
# No admin rights needed. Output: build\uiaccess\Veil.exe + build\uiaccess\VeilDev.cer
#
# The portable build (build.bat) is untouched: the manifest is embedded only here, by swapping
# rsrc_windows_amd64.syso for the duration of the build and restoring it afterwards.
# (This file is ASCII on purpose: Windows PowerShell 5.1 misreads UTF-8 without BOM.)

# -NoSign: build only (CI / installer input; the installer signs on the user's machine).
param([switch]$NoSign)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$out  = Join-Path $root 'build\uiaccess'
$syso = Join-Path $root 'rsrc_windows_amd64.syso'
$bak  = Join-Path $out  'rsrc_portable.syso.bak'
$subject = 'CN=Veil Dev Code Signing'

New-Item -ItemType Directory -Force -Path $out | Out-Null
if (Test-Path $bak) { throw "Stale backup $bak found: a previous build was interrupted. Restore it over $syso first." }

Push-Location $root
try {
    Copy-Item $syso $bak
    try {
        & rsrc -arch amd64 -manifest veil.uiaccess.manifest -ico app.ico -o $syso
        if ($LASTEXITCODE -ne 0) { throw 'rsrc failed' }
        & go build -trimpath -ldflags '-H=windowsgui -s -w' -o (Join-Path $out 'Veil.exe') .
        if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
    } finally {
        Copy-Item $bak $syso -Force
        Remove-Item $bak
    }
} finally {
    Pop-Location
}

if ($NoSign) {
    Write-Host "BUILD OK  $out\Veil.exe (unsigned)"
    return
}

# Self-signed code signing cert, kept in the current user's store with a non-exportable key.
$cert = Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert |
    Where-Object { $_.Subject -eq $subject -and $_.NotAfter -gt (Get-Date) } | Select-Object -First 1
if (-not $cert) {
    $cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject $subject `
        -CertStoreLocation Cert:\CurrentUser\My -KeyExportPolicy NonExportable `
        -NotAfter (Get-Date).AddYears(3)
    Write-Host "Created cert $($cert.Thumbprint)"
}
Export-Certificate -Cert $cert -FilePath (Join-Path $out 'VeilDev.cer') | Out-Null

$sig = Set-AuthenticodeSignature -FilePath (Join-Path $out 'Veil.exe') -Certificate $cert -HashAlgorithm SHA256
if (-not $sig.SignerCertificate) { throw "Signing failed: $($sig.StatusMessage)" }

Write-Host "BUILD OK  $out\Veil.exe"
Write-Host "Signed by $($cert.Thumbprint) (status: $($sig.Status); 'UnknownError' is expected until deploy-uiaccess.ps1 trusts the cert)"
