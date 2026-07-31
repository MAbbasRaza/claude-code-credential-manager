; NSIS installer for ccm.
;
; Always built through scripts/build-installer.ps1, which is the only place
; makensis is invoked so a local build and the release build cannot drift. It
; derives VERSIONQUAD with scripts/relver and passes absolute paths:
;
;   pwsh -File scripts/build-installer.ps1 -Build
;
; Deliberately a per-user install. RequestExecutionLevel user means no UAC
; prompt and no administrator account required, which matches how the rest of
; ccm works: everything lives under the user profile, and the vault it opens is
; bound to one user by DPAPI. A machine-wide install would be actively wrong,
; not merely unnecessary.

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
; VIProductVersion needs exactly four numeric parts and will not compile
; without them, so a tag like v0.3.0-rc1 cannot be used directly. relver
; supplies this separately from the display version.
!ifndef VERSIONQUAD
  !define VERSIONQUAD "0.0.0.0"
!endif
!ifndef SRCDIR
  !define SRCDIR "..\..\bin"
!endif
; The published asset name carries no version. GitHub's
; /releases/latest/download/<name> redirect only resolves a fixed filename, and
; that redirect is what lets the docs link a permanent download URL.
!ifndef OUTFILE
  !define OUTFILE "..\..\dist\ccm-setup-windows.exe"
!endif

!define APPNAME    "Claude Code Multi-Account Manager"
!define SHORTNAME  "ccm"
!define PUBLISHER  "MAbbasRaza"
!define HOMEPAGE   "https://github.com/MAbbasRaza/claude-code-multi-account-manager"
!define UNINSTKEY  "Software\Microsoft\Windows\CurrentVersion\Uninstall\${SHORTNAME}"

Name "${APPNAME}"
OutFile "${OUTFILE}"
Unicode True
RequestExecutionLevel user
InstallDir "$LOCALAPPDATA\Programs\ccm"
InstallDirRegKey HKCU "Software\${SHORTNAME}" "InstallDir"
ShowInstDetails show
ShowUnInstDetails show

VIProductVersion "${VERSIONQUAD}"
VIAddVersionKey "ProductName"     "${APPNAME}"
VIAddVersionKey "CompanyName"     "${PUBLISHER}"
VIAddVersionKey "FileDescription" "Switch between Claude Code accounts without signing in again"
VIAddVersionKey "FileVersion"     "${VERSION}"
VIAddVersionKey "ProductVersion"  "${VERSION}"
VIAddVersionKey "LegalCopyright"  "MIT"

!define MUI_ICON   "..\..\assets\icon.ico"
!define MUI_UNICON "..\..\assets\icon.ico"
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_LICENSE "..\..\LICENSE"
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES

!define MUI_FINISHPAGE_RUN "$INSTDIR\ccm-gui.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open Claude Code Accounts"
!define MUI_FINISHPAGE_LINK "Setup guide"
!define MUI_FINISHPAGE_LINK_LOCATION "${HOMEPAGE}/blob/main/docs/SETUP.md"
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

; ---------------------------------------------------------------------------
; PATH handling
;
; Read and written through the registry rather than any expand-on-read helper.
; The user PATH is REG_EXPAND_SZ on a default Windows install, and rewriting it
; as REG_SZ stops the system expanding %VAR% entries it contains, silently
; breaking unrelated tools. WriteRegExpandStr preserves the type.
; ---------------------------------------------------------------------------

!macro BroadcastEnvChange
  ; WM_SETTINGCHANGE so newly launched shells see the change without a logout.
  SendMessage ${HWND_BROADCAST} ${WM_WININICHANGE} 0 "STR:Environment" /TIMEOUT=3000
!macroend

Function AddToPath
  Push $0
  Push $1
  ReadRegStr $0 HKCU "Environment" "Path"

  ; Already present? Compared with delimiters on both sides so "…\ccm2" cannot
  ; be mistaken for "…\ccm".
  StrCpy $1 ";$0;"
  Push $1
  Push ";$INSTDIR;"
  Call StrContains
  Pop $1
  ${If} $1 != ""
    Pop $1
    Pop $0
    Return
  ${EndIf}

  ${If} $0 == ""
    WriteRegExpandStr HKCU "Environment" "Path" "$INSTDIR"
  ${Else}
    WriteRegExpandStr HKCU "Environment" "Path" "$0;$INSTDIR"
  ${EndIf}
  !insertmacro BroadcastEnvChange
  Pop $1
  Pop $0
FunctionEnd

Function un.RemoveFromPath
  Push $0
  Push $1
  ReadRegStr $0 HKCU "Environment" "Path"
  ${If} $0 == ""
    Pop $1
    Pop $0
    Return
  ${EndIf}

  ; Rebuild without our entry rather than string-replacing, which would leave
  ; a stray semicolon or clip a similarly named directory.
  Push $0
  Push "$INSTDIR"
  Call un.RemovePathEntry
  Pop $1

  WriteRegExpandStr HKCU "Environment" "Path" "$1"
  !insertmacro BroadcastEnvChange
  Pop $1
  Pop $0
FunctionEnd

; StrContains: [haystack] [needle] -> "" when absent, needle when present
Function StrContains
  Exch $R0 ; needle
  Exch
  Exch $R1 ; haystack
  Push $R2
  Push $R3
  Push $R4
  StrLen $R2 $R0
  StrCpy $R3 0
  loop:
    StrCpy $R4 $R1 $R2 $R3
    ${If} $R4 == $R0
      StrCpy $R0 $R4
      Goto done
    ${EndIf}
    ${If} $R4 == ""
      StrCpy $R0 ""
      Goto done
    ${EndIf}
    IntOp $R3 $R3 + 1
    Goto loop
  done:
  Pop $R4
  Pop $R3
  Pop $R2
  Pop $R1
  Exch $R0
FunctionEnd

; un.RemovePathEntry: [path] [entry] -> path without entry
Function un.RemovePathEntry
  Exch $R0 ; entry
  Exch
  Exch $R1 ; path
  Push $R2 ; rebuilt
  Push $R3 ; current segment
  Push $R4 ; char
  Push $R5 ; index

  StrCpy $R2 ""
  StrCpy $R3 ""
  StrCpy $R5 0

  loop:
    StrCpy $R4 $R1 1 $R5
    ${If} $R4 == ";"
    ${OrIf} $R4 == ""
      ${If} $R3 != ""
      ${AndIf} $R3 != $R0
        ${If} $R2 == ""
          StrCpy $R2 "$R3"
        ${Else}
          StrCpy $R2 "$R2;$R3"
        ${EndIf}
      ${EndIf}
      StrCpy $R3 ""
      ${If} $R4 == ""
        Goto done
      ${EndIf}
    ${Else}
      StrCpy $R3 "$R3$R4"
    ${EndIf}
    IntOp $R5 $R5 + 1
    Goto loop
  done:

  StrCpy $R0 $R2
  Pop $R5
  Pop $R4
  Pop $R3
  Pop $R2
  Pop $R1
  Exch $R0
FunctionEnd

; ---------------------------------------------------------------------------
; Sections
; ---------------------------------------------------------------------------

Section "Command line tool (required)" SecCore
  SectionIn RO
  SetOutPath "$INSTDIR"
  File "${SRCDIR}\ccm.exe"

  WriteRegStr HKCU "Software\${SHORTNAME}" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "Software\${SHORTNAME}" "Version" "${VERSION}"

  Call AddToPath

  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; Registered under HKCU so it appears in Settings > Apps without elevation.
  WriteRegStr   HKCU "${UNINSTKEY}" "DisplayName"     "${APPNAME}"
  WriteRegStr   HKCU "${UNINSTKEY}" "DisplayVersion"  "${VERSION}"
  WriteRegStr   HKCU "${UNINSTKEY}" "Publisher"       "${PUBLISHER}"
  WriteRegStr   HKCU "${UNINSTKEY}" "DisplayIcon"     "$INSTDIR\ccm.exe"
  WriteRegStr   HKCU "${UNINSTKEY}" "URLInfoAbout"    "${HOMEPAGE}"
  WriteRegStr   HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr   HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1

  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINSTKEY}" "EstimatedSize" "$0"
SectionEnd

Section "Desktop app" SecGui
  SetOutPath "$INSTDIR"
  File "${SRCDIR}\ccm-gui.exe"
  CreateDirectory "$SMPROGRAMS\${APPNAME}"
  CreateShortcut "$SMPROGRAMS\${APPNAME}\Claude Code Accounts.lnk" "$INSTDIR\ccm-gui.exe"
SectionEnd

Section "System tray app" SecTray
  SetOutPath "$INSTDIR"
  File "${SRCDIR}\ccm-tray.exe"
  CreateDirectory "$SMPROGRAMS\${APPNAME}"
  CreateShortcut "$SMPROGRAMS\${APPNAME}\Claude Code Accounts (tray).lnk" "$INSTDIR\ccm-tray.exe"
SectionEnd

Section "Start the tray app when I log in" SecBoot
  ; Delegated to ccm rather than written here, so there is one implementation
  ; of what "start at login" means and the app's own toggle reads back exactly
  ; what the installer wrote.
  DetailPrint "Registering start at login"
  nsExec::ExecToLog '"$INSTDIR\ccm.exe" autostart enable'
  Pop $0
  ${If} $0 != 0
    DetailPrint "Could not register start at login (exit $0). Enable it later from the app's Settings."
  ${EndIf}
SectionEnd

; Selected by default; the user can untick either on the components page.
!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecCore} "The ccm command, added to your PATH."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecGui}  "A window for managing accounts: switch, capture, rename, remove."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecTray} "A tray icon for switching without opening anything."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecBoot} "Runs the tray app at login so switching is always available. You can change this later in Settings."
!insertmacro MUI_FUNCTION_DESCRIPTION_END

Function .onInit
  ; Start at login depends on the tray, so it cannot be on without it.
  ; Enforced in .onSelChange rather than merely documented.
  SectionSetFlags ${SecBoot} ${SF_SELECTED}
FunctionEnd

Function .onSelChange
  ${IfNot} ${SectionIsSelected} ${SecTray}
    !insertmacro UnselectSection ${SecBoot}
    SectionSetFlags ${SecBoot} ${SF_RO}
  ${Else}
    SectionSetFlags ${SecBoot} 0
  ${EndIf}
FunctionEnd

; ---------------------------------------------------------------------------
; Uninstaller
; ---------------------------------------------------------------------------

Section "Uninstall"
  ; Before deleting the binary that implements it.
  nsExec::ExecToLog '"$INSTDIR\ccm.exe" autostart disable'
  Pop $0

  ; A running tray or app holds its own exe open, so a delete would fail.
  nsExec::ExecToLog 'taskkill /IM ccm-tray.exe /F'
  Pop $0
  nsExec::ExecToLog 'taskkill /IM ccm-gui.exe /F'
  Pop $0

  Delete "$INSTDIR\ccm.exe"
  Delete "$INSTDIR\ccm-tray.exe"
  Delete "$INSTDIR\ccm-gui.exe"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${APPNAME}\Claude Code Accounts.lnk"
  Delete "$SMPROGRAMS\${APPNAME}\Claude Code Accounts (tray).lnk"
  RMDir "$SMPROGRAMS\${APPNAME}"

  Call un.RemoveFromPath

  DeleteRegKey HKCU "${UNINSTKEY}"
  DeleteRegKey HKCU "Software\${SHORTNAME}"

  ; The vault and settings are deliberately left in place. They hold the user's
  ; saved accounts, and an uninstall that silently destroyed them would force a
  ; browser sign-in for every account to recover. Removal is documented instead.
  DetailPrint "Your saved accounts in %LOCALAPPDATA%\ccm were left in place."
SectionEnd
