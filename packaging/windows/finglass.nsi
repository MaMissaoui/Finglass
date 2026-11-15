; Finglass Windows installer (per-user, no administrator rights).
;
; Built but not released: scripts/go-package-windows.sh passes no FinTS
; product id. WebView2 is checked, never downloaded or bundled; the person is
; pointed at Microsoft's download page when the Evergreen runtime is missing.
;
; The uninstaller removes the program files, the Start-menu shortcut and the
; HKCU uninstall key. It never touches %USERPROFILE%\.finglass, which holds the
; person's financial data (see the uninstall section).
;
; Build with:
;   makensis -DVERSION=... -DSRCDIR=... -DNOTICES=... -DICON=... -DOUTFILE=... finglass.nsi

Unicode true
!include "MUI2.nsh"

!ifndef VERSION
  !define VERSION "0.0.0-dev"
!endif
!ifndef SRCDIR
  !define SRCDIR "build\go\windows"
!endif
!ifndef NOTICES
  !define NOTICES "packaging\THIRD-PARTY-NOTICES.txt"
!endif
!ifndef ICON
  !define ICON "assets\icons\finglass.ico"
!endif
!ifndef OUTFILE
  !define OUTFILE "build\go\Finglass_${VERSION}_windows_amd64_setup.exe"
!endif

Name "Finglass"
Caption "Finglass ${VERSION} setup"
OutFile "${OUTFILE}"

; Per-user install: no administrator prompt and no machine-wide state.
RequestExecutionLevel user
InstallDir "$LOCALAPPDATA\Programs\Finglass"
InstallDirRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Finglass" "InstallLocation"

SetCompressor /SOLID lzma
Icon "${ICON}"
UninstallIcon "${ICON}"

!define MUI_ABORTWARNING
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "French"

LangString webview2Missing ${LANG_ENGLISH} "Finglass needs the Microsoft Edge WebView2 runtime. Install it from https://developer.microsoft.com/microsoft-edge/webview2/ and run this installer again."
LangString webview2Missing ${LANG_GERMAN} "Finglass braucht die Microsoft Edge WebView2-Laufzeit. Installieren Sie sie von https://developer.microsoft.com/microsoft-edge/webview2/ und starten Sie dieses Installationsprogramm erneut."
LangString webview2Missing ${LANG_FRENCH} "Finglass a besoin du runtime Microsoft Edge WebView2. Installez-le depuis https://developer.microsoft.com/microsoft-edge/webview2/ puis relancez ce programme d'installation."

; CheckPvValue reads $0: a missing value, an empty value or 0.0.0.0 means the
; runtime is not usable. $1 becomes "present" only for a real version.
Function CheckPvValue
  StrCmp $0 "" checkpv_done
  StrCmp $0 "0.0.0.0" checkpv_done
  StrCpy $1 "present"
checkpv_done:
FunctionEnd

; The Evergreen runtime registers its version under HKLM/HKCU, in the native
; and the 32-bit registry views. Check both; never download anything here.
Function CheckWebView2
  StrCpy $1 "missing"

  SetRegView 32
  ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  Call CheckPvValue
  StrCmp $1 "present" checkwebview2_done
  ReadRegStr $0 HKLM "SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  Call CheckPvValue
  StrCmp $1 "present" checkwebview2_done
  ReadRegStr $0 HKCU "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  Call CheckPvValue
  StrCmp $1 "present" checkwebview2_done
  ReadRegStr $0 HKCU "SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  Call CheckPvValue
  StrCmp $1 "present" checkwebview2_done

  SetRegView 64
  ReadRegStr $0 HKLM "SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  Call CheckPvValue
  StrCmp $1 "present" checkwebview2_done
  ReadRegStr $0 HKCU "SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  Call CheckPvValue

checkwebview2_done:
  SetRegView 32
FunctionEnd

Function .onInit
  SetShellVarContext current
  Call CheckWebView2
  StrCmp $1 "present" webview2_ok

  IfSilent silent_abort
  MessageBox MB_ICONSTOP "$(webview2Missing)"
  Goto abort

silent_abort:
  ; /S has no UI, so leave the reason in a log next to the temporary files.
  FileOpen $0 "$TEMP\Finglass-install.log" w
  FileWrite $0 "$(webview2Missing)$\r$\n"
  FileClose $0

abort:
  SetErrorLevel 2
  Quit

webview2_ok:
FunctionEnd

Section "Finglass" SecMain
  SetOutPath "$INSTDIR"
  File "${SRCDIR}\Finglass.exe"
  File /oname=THIRD-PARTY-NOTICES.txt "${NOTICES}"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateDirectory "$SMPROGRAMS\Finglass"
  CreateShortCut "$SMPROGRAMS\Finglass\Finglass.lnk" "$INSTDIR\Finglass.exe"

  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Finglass" "DisplayName" "Finglass"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Finglass" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Finglass" "Publisher" "Finglass"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Finglass" "DisplayIcon" "$INSTDIR\Finglass.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Finglass" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Finglass" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Finglass" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Finglass" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Finglass" "NoRepair" 1
SectionEnd

Section "Uninstall"
  SetShellVarContext current

  Delete "$INSTDIR\Finglass.exe"
  Delete "$INSTDIR\THIRD-PARTY-NOTICES.txt"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\Finglass\Finglass.lnk"
  RMDir "$SMPROGRAMS\Finglass"

  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Finglass"

  ; Never delete "$PROFILE\.finglass" or anything below it. That folder holds
  ; the person's financial data; uninstalling the program must leave it alone.
SectionEnd
