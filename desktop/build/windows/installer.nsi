Unicode True
!include "MUI2.nsh"
!include "x64.nsh"

!ifndef VERSION
  !error "VERSION is required"
!endif
!ifndef PAYLOAD
  !error "PAYLOAD is required"
!endif
!ifndef OUTPUT
  !error "OUTPUT is required"
!endif

Name "Cloaksession"
OutFile "${OUTPUT}"
InstallDir "$LOCALAPPDATA\Cloaksession"
InstallDirRegKey HKCU "Software\Cloaksession" "InstallLocation"
RequestExecutionLevel user
SetCompressor /SOLID lzma
VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=1033 "ProductName" "Cloaksession"
VIAddVersionKey /LANG=1033 "FileDescription" "Cloaksession Setup"
VIAddVersionKey /LANG=1033 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=1033 "LegalCopyright" "Copyright 2026 Cloaksession contributors"

!define MUI_ABORTWARNING
!define MUI_ICON "..\..\icons\icon.ico"
!define MUI_UNICON "..\..\icons\icon.ico"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "..\..\..\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\Cloaksession.exe"
!define MUI_FINISHPAGE_RUN_NOTCHECKED
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "Cloaksession requires 64-bit Windows."
    Abort
  ${EndIf}
  SetRegView 64
  SetShellVarContext current
FunctionEnd

Function EnsureAppClosed
  check:
    FindWindow $0 "" "Cloaksession"
    StrCmp $0 0 check_desktop busy
  check_desktop:
    IfFileExists "$INSTDIR\Cloaksession.exe" 0 check_core
    System::Call 'kernel32::CreateFileW(w "$INSTDIR\Cloaksession.exe", i 0x40000000, i 0, p 0, i 3, i 0, p 0) p.r1'
    StrCmp $1 -1 busy
    System::Call 'kernel32::CloseHandle(p r1)'
  check_core:
    ; A previous installation may still have its retired helper running.
    IfFileExists "$INSTDIR\desktop-core.exe" 0 done
    System::Call 'kernel32::CreateFileW(w "$INSTDIR\desktop-core.exe", i 0x40000000, i 0, p 0, i 3, i 0, p 0) p.r1'
    StrCmp $1 -1 busy
    System::Call 'kernel32::CloseHandle(p r1)'
    Goto done
  busy:
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "Close all browser sessions and fully exit Cloaksession before updating. If an executable is still locked, wait for the application and any previous-version helper to exit or close them in Task Manager. Check that the install folder is writable, then click Retry. Cancel leaves the installed files unchanged." /SD IDCANCEL IDRETRY check
    Abort
  done:
FunctionEnd

Section "Cloaksession" SEC_MAIN
  Call EnsureAppClosed
  SetOutPath "$INSTDIR"
  File "${PAYLOAD}\Cloaksession.exe"
  Delete "$INSTDIR\desktop-core.exe"
  SetOutPath "$INSTDIR\resources"
  File "${PAYLOAD}\resources\LICENSE"
  ; Replace runtime dependencies, never the separate user data directory.
  RMDir /r "$INSTDIR\resources\chromix"
  RMDir /r "$INSTDIR\resources\playwright"
  SetOutPath "$INSTDIR\resources\playwright"
  File /r "${PAYLOAD}\resources\playwright\*.*"
  RMDir /r "$INSTDIR\resources\reverse"
  SetOutPath "$INSTDIR\resources\reverse"
  File /r "${PAYLOAD}\resources\reverse\*.*"
  RMDir /r "$INSTDIR\resources\companion"
  SetOutPath "$INSTDIR\resources\companion"
  File /r "${PAYLOAD}\resources\companion\*.*"
  SetOutPath "$INSTDIR"
  WriteUninstaller "$INSTDIR\uninstall.exe"
  CreateShortcut "$SMPROGRAMS\Cloaksession.lnk" "$INSTDIR\Cloaksession.exe"
  CreateShortcut "$DESKTOP\Cloaksession.lnk" "$INSTDIR\Cloaksession.exe"
  WriteRegStr HKCU "Software\Cloaksession" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Cloaksession" "DisplayName" "Cloaksession"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Cloaksession" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Cloaksession" "Publisher" "Cloaksession contributors"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Cloaksession" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Cloaksession" "DisplayIcon" "$INSTDIR\Cloaksession.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Cloaksession" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Cloaksession" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Cloaksession" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Cloaksession" "NoRepair" 1
SectionEnd

Function un.onInit
  SetRegView 64
  SetShellVarContext current
FunctionEnd

Section "Uninstall"
  Delete "$SMPROGRAMS\Cloaksession.lnk"
  Delete "$DESKTOP\Cloaksession.lnk"
  Delete "$INSTDIR\Cloaksession.exe"
  Delete "$INSTDIR\desktop-core.exe"
  Delete "$INSTDIR\resources\LICENSE"
  RMDir /r "$INSTDIR\resources\chromix"
  RMDir /r "$INSTDIR\resources\playwright"
  RMDir /r "$INSTDIR\resources\companion"
  RMDir /r "$INSTDIR\resources\reverse"
  RMDir "$INSTDIR\resources"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Cloaksession"
  DeleteRegKey HKCU "Software\Cloaksession"
SectionEnd
