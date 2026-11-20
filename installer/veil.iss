; Veil installer (Inno Setup 6). Build:  ISCC /DAppVersion=1.1.0 installer\veil.iss
; Input: build\uiaccess\Veil.exe built by  scripts\build-uiaccess.ps1 -NoSign  (unsigned on purpose:
; the installer signs it on the user's machine, see install-sign.ps1).

#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif

[Setup]
AppId={{6B0C3E52-6C4E-4F0B-9E0B-5A7C1D2F7F3A}
AppName=Veil
AppVersion={#AppVersion}
AppPublisher=Airlur
AppPublisherURL=https://github.com/Airlur/Veil
; UIAccess only works from a secure location, so the directory is fixed to Program Files.
DefaultDirName={autopf}\Veil
DisableDirPage=yes
DisableProgramGroupPage=yes
PrivilegesRequired=admin
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
MinVersion=10.0.19041
OutputDir=..\build\installer
OutputBaseFilename=Veil-Setup
SetupIconFile=..\app.ico
UninstallDisplayIcon={app}\Veil.exe
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=no

[Messages]
WelcomeLabel2=This will install [name/ver] on your computer.%n%nVeil needs the Windows "UIAccess" permission to cover IME candidate windows and context menus. Windows only grants it to signed programs, so Setup will create a self-signed certificate on this computer, sign Veil.exe with it, add the certificate to the trusted root store and delete its private key. Uninstalling removes the certificate.

[Files]
Source: "..\build\uiaccess\Veil.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "install-sign.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Veil"; Filename: "{app}\Veil.exe"

[Run]
; shellexec + runasoriginaluser: a UIAccess program must be started through ShellExecute and unelevated.
Filename: "{app}\Veil.exe"; Description: "Launch Veil"; Flags: postinstall nowait shellexec runasoriginaluser skipifsilent

[UninstallRun]
Filename: "powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\install-sign.ps1"" -Exe ""{app}\Veil.exe"" -Uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "VeilRemoveCert"

[UninstallDelete]
Type: files; Name: "{app}\install-sign.log"

[Code]
procedure StopVeil;
var
  ResultCode: Integer;
begin
  // Killing the main process is safe: its cursor watchdog restores the mouse cursor.
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM Veil.exe', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(500);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  StopVeil;
  Result := '';
end;

function InitializeUninstall: Boolean;
begin
  StopVeil;
  Result := True;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
  Params: String;
begin
  if CurStep = ssPostInstall then
  begin
    Params := '-NoProfile -ExecutionPolicy Bypass -File "' + ExpandConstant('{app}\install-sign.ps1') +
              '" -Exe "' + ExpandConstant('{app}\Veil.exe') + '"';
    if (not Exec('powershell.exe', Params, '', SW_HIDE, ewWaitUntilTerminated, ResultCode)) or (ResultCode <> 0) then
      MsgBox('Signing Veil.exe failed, so Veil will not start.' + #13#10 +
             'See ' + ExpandConstant('{app}\install-sign.log') + ' for details.', mbError, MB_OK);
  end;
end;
