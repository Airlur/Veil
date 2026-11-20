# install-sign.ps1 - run by the installer (elevated) to make Veil.exe eligible for UIAccess.
#
# Windows only grants UIAccess to executables whose signature chains to a root this machine
# trusts. Instead of shipping a shared signing key, the installer signs locally:
#   1. create a one-off self-signed code signing certificate on THIS machine
#   2. sign Veil.exe with it
#   3. add the certificate (public part only) to LocalMachine\Root
#   4. delete the private key, so nothing else can ever be signed with the trusted certificate
#
#   -Uninstall   remove the certificate(s) from LocalMachine\Root
#   -TestMode    dry run without admin: uses CurrentUser\My and skips step 3
#
# (ASCII on purpose: Windows PowerShell 5.1 misreads UTF-8 without BOM.)

param(
    [Parameter(Mandatory = $true)][string]$Exe,
    [switch]$Uninstall,
    [switch]$TestMode
)

$ErrorActionPreference = 'Stop'
$subject = 'CN=Veil Local Signing'
$my = if ($TestMode) { 'Cert:\CurrentUser\My' } else { 'Cert:\LocalMachine\My' }
$log = Join-Path (Split-Path -Parent $Exe) 'install-sign.log'

function Write-Log([string]$msg) {
    $line = '[{0}] {1}' -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $msg
    Write-Host $line
    try { Add-Content -Path $log -Value $line -Encoding UTF8 } catch { }
}

function Remove-TrustedCerts {
    if ($TestMode) { return }
    Get-ChildItem Cert:\LocalMachine\Root | Where-Object { $_.Subject -eq $subject } | ForEach-Object {
        Write-Log "removing trusted cert $($_.Thumbprint)"
        Remove-Item $_.PSPath
    }
}

try {
    if ($Uninstall) {
        Remove-TrustedCerts
        Write-Log 'uninstall: done'
        exit 0
    }

    if (-not (Test-Path $Exe)) { throw "not found: $Exe" }
    Remove-TrustedCerts   # certificates left by a previous install

    $cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject $subject `
        -CertStoreLocation $my -KeyExportPolicy NonExportable -NotAfter (Get-Date).AddYears(20)
    Write-Log "created cert $($cert.Thumbprint)"
    try {
        $sig = Set-AuthenticodeSignature -FilePath $Exe -Certificate $cert -HashAlgorithm SHA256
        if (-not $sig.SignerCertificate) { throw "signing failed: $($sig.StatusMessage)" }

        if (-not $TestMode) {
            $cer = Join-Path $env:TEMP ("veil-" + $cert.Thumbprint + ".cer")
            Export-Certificate -Cert $cert -FilePath $cer | Out-Null
            Import-Certificate -FilePath $cer -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
            Remove-Item $cer
        }
    } finally {
        # Drop the private key whatever happened above.
        Remove-Item (Join-Path $my $cert.Thumbprint) -DeleteKey
        Write-Log 'private key deleted'
    }

    $check = Get-AuthenticodeSignature $Exe
    Write-Log "signature status: $($check.Status), signer: $($check.SignerCertificate.Thumbprint)"
    if ($check.SignerCertificate.Thumbprint -ne $cert.Thumbprint) { throw 'signature check failed' }
    if (-not $TestMode -and $check.Status -ne 'Valid') { throw "signature not trusted: $($check.StatusMessage)" }
    exit 0
} catch {
    Write-Log "FAILED: $_"
    exit 1
}
