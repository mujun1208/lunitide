Unicode true
RequestExecutionLevel user
!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "nsDialogs.nsh"
!include "WordFunc.nsh"
!insertmacro VersionCompare
!insertmacro GetFileName
!insertmacro GetParent
!define APPID "Lunitide.Desktop.7A565D82-936E-4E06-962D-83B5DD24E53C"
!define OWNERFILE ".lunitide-install-owner"
!define PRODUCT "Lunitide 月汐"
Name "${PRODUCT}"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\Lunitide"
InstallDirRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "InstallLocation"
SetCompressor /SOLID lzma
!define MUI_ABORTWARNING
!define MUI_ICON "${STAGE}\lunitide-icon.ico"
!define MUI_UNICON "${STAGE}\lunitide-icon.ico"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_WELCOME
UninstPage custom un.PurgePage un.PurgeLeave
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_UNPAGE_FINISH
!insertmacro MUI_LANGUAGE "English"
Var PurgeCheck
Var PurgeData
Var AllowDowngrade
Var InstallStage
Var InstallBackup
Var PreviousVersion
Var PreviousRegistration
Var PreviousInstall
Var InstallParent
Var InstallLeaf
Var InstallLog
Var PurgeAttempts
!macro Log MESSAGE
  FileOpen $9 "$InstallLog" a
  FileSeek $9 0 END
  FileWrite $9 "${MESSAGE}$\r$\n"
  FileClose $9
!macroend
Function .onInit
  StrCpy $AllowDowngrade 0
  StrCpy $PreviousRegistration 0
  StrCpy $PreviousVersion ""
  StrCpy $PreviousInstall 0
  CreateDirectory "$LOCALAPPDATA\LunitideInstaller\Logs"
  StrCpy $InstallLog "$LOCALAPPDATA\LunitideInstaller\Logs\install-latest.log"
  FileOpen $9 "$InstallLog" w
  FileWrite $9 "event=start version=${VERSION}$\r$\n"
  FileClose $9
  ${GetParameters} $0
  ${GetOptions} $0 "/ALLOWDOWNGRADE" $1
  ${IfNot} ${Errors}
    StrCpy $AllowDowngrade 1
  ${EndIf}
FunctionEnd
Section "Install"
  ${GetParent} "$INSTDIR" $InstallParent
  ${GetFileName} "$INSTDIR" $InstallLeaf
  StrCmp $InstallParent "" invalid_install_path
  StrCmp $InstallLeaf "" invalid_install_path
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "InstallLocation"
  StrCmp $0 "" target_selected
  StrCmp $0 "$INSTDIR" target_selected registered_path_mismatch
registered_path_mismatch:
  !insertmacro Log "event=failure code=I101 phase=target reason=registered-location-mismatch selected=$INSTDIR registered=$0"
  SetErrorLevel 10
  Abort "An existing Lunitide installation must be upgraded in place. Uninstall it first to choose a different drive. Diagnostic log: $InstallLog"
invalid_install_path:
  !insertmacro Log "event=failure code=I100 phase=target reason=invalid-path path=$INSTDIR"
  SetErrorLevel 10
  Abort "Choose a folder below a local fixed drive, for example D:\Apps\Lunitide. Diagnostic log: $InstallLog"
target_selected:
  !insertmacro Log "event=target path=$INSTDIR parent=$InstallParent"
  IfFileExists "$INSTDIR\*.*" 0 ownership_ok
  ClearErrors
  FileOpen $0 "$INSTDIR\${OWNERFILE}" r
  IfErrors legacy_owner
  FileRead $0 $1
  FileRead $0 $2
  FileClose $0
  StrCmp $1 "${APPID}" 0 ownership_invalid
  StrCmp $2 "" ownership_version ownership_invalid
legacy_owner:
  ReadRegStr $1 HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "InstallLocation"
  StrCmp $1 "$INSTDIR" ownership_version ownership_invalid
ownership_invalid:
  !insertmacro Log "event=failure code=I110 phase=ownership path=$INSTDIR"
  SetErrorLevel 11
  Abort "The selected folder is not empty and is not owned by Lunitide. Choose an empty folder. Diagnostic log: $InstallLog"
ownership_version:
  ReadRegStr $1 HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "DisplayVersion"
  StrCmp $1 "" ownership_ok
  StrCpy $PreviousRegistration 1
  StrCpy $PreviousVersion $1
  ${VersionCompare} $1 "${VERSION}" $2
  StrCmp $2 1 0 ownership_ok
  StrCmp $AllowDowngrade 1 ownership_ok
  !insertmacro Log "event=failure code=I111 phase=version reason=downgrade"
  SetErrorLevel 12
  Abort "Downgrade refused. Re-run with /ALLOWDOWNGRADE to explicitly permit it. Diagnostic log: $InstallLog"
ownership_ok:
  InitPluginsDir
  ClearErrors
  CreateDirectory "$InstallParent"
  IfErrors parent_failed
  SetOutPath "$PLUGINSDIR"
  File /oname=stop-install-processes.ps1 "${STAGE}\stop-install-processes.ps1"
  File /oname=verify-install-directory.ps1 "${STAGE}\verify-install-directory.ps1"
  ${GetFileName} "$PLUGINSDIR" $1
  StrCpy $InstallStage "$InstallParent\$InstallLeaf.installing.$1"
  StrCpy $InstallBackup "$InstallParent\$InstallLeaf.backup.$1"
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\verify-install-directory.ps1" -Path "$INSTDIR" -LogPath "$InstallLog"'
  Pop $0
  StrCmp $0 0 target_path_ok
  StrCmp $0 33 target_path_ok unsafe_path
target_path_ok:
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\verify-install-directory.ps1" -Path "$InstallStage" -LogPath "$InstallLog"'
  Pop $0
  StrCmp $0 0 +2
  Goto unsafe_path
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\verify-install-directory.ps1" -Path "$InstallBackup" -LogPath "$InstallLog"'
  Pop $0
  StrCmp $0 0 +2
  Goto unsafe_path
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\stop-install-processes.ps1" -Path "$INSTDIR" -LogPath "$InstallLog"'
  Pop $0
  StrCmp $0 0 stop_ok
  !insertmacro Log "event=failure code=I120 phase=stop-processes helper-exit=$0"
  SetErrorLevel 14
  Abort "Unable to stop the existing Lunitide processes; no files were replaced. Close Lunitide and retry. Diagnostic log: $InstallLog"
unsafe_path:
  !insertmacro Log "event=failure code=I130 phase=path-validation helper-exit=$0"
  SetErrorLevel 13
  Abort "The selected installation or temporary replacement path is unsafe. Diagnostic log: $InstallLog"
parent_failed:
  !insertmacro Log "event=failure code=I131 phase=create-parent path=$InstallParent"
  SetErrorLevel 13
  Abort "Unable to create the selected installation folder. Check that the drive is writable. Diagnostic log: $InstallLog"
stop_ok:
  ClearErrors
  CreateDirectory "$InstallStage"
  IfErrors stage_failed
	nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\verify-install-directory.ps1" -Path "$InstallStage" -MustExist -LogPath "$InstallLog"'
  Pop $0
  StrCmp $0 0 +2
  Goto stage_failed
	SetOutPath "$InstallStage"
  File /r "${STAGE}\*"
	IfErrors stage_failed
	IfFileExists "$InstallStage\Lunitide.exe" 0 stage_failed
	IfFileExists "$InstallStage\lunitide-engine.exe" 0 stage_failed
	IfFileExists "$InstallStage\SHA256SUMS.txt" 0 stage_failed
	FileOpen $0 "$InstallStage\${OWNERFILE}" w
	IfErrors stage_failed
  FileWrite $0 "${APPID}"
	IfErrors stage_failed_open
  FileClose $0
	SetOutPath "$InstallStage"
	WriteUninstaller "$InstallStage\Uninstall.exe"
	IfErrors stage_failed
	nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\verify-install-directory.ps1" -Path "$InstallStage" -MustExist -LogPath "$InstallLog"'
	Pop $0
	StrCmp $0 0 +2
	Goto stage_failed
	SetOutPath "$PLUGINSDIR"
	IfFileExists "$INSTDIR\." 0 activate_stage
	ClearErrors
	Rename "$INSTDIR" "$InstallBackup"
	IfErrors swap_failed
  StrCpy $PreviousInstall 1
activate_stage:
	ClearErrors
	Rename "$InstallStage" "$INSTDIR"
	IfErrors activate_failed
  ClearErrors
  CreateDirectory "$SMPROGRAMS\Lunitide"
  IfErrors commit_failed
  ClearErrors
  CreateShortcut "$SMPROGRAMS\Lunitide\Lunitide.lnk" "$INSTDIR\Lunitide.exe" "" "$INSTDIR\lunitide-icon.ico" 0
  IfErrors commit_failed
  ClearErrors
  CreateShortcut "$DESKTOP\Lunitide.lnk" "$INSTDIR\Lunitide.exe" "" "$INSTDIR\lunitide-icon.ico" 0
  IfErrors commit_failed
  ClearErrors
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "DisplayName" "${PRODUCT}"
  IfErrors commit_failed
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "DisplayVersion" "${VERSION}"
  IfErrors commit_failed
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "InstallLocation" "$INSTDIR"
  IfErrors commit_failed
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  IfErrors commit_failed
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  IfErrors commit_failed
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "NoModify" 1
  IfErrors commit_failed
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "NoRepair" 1
  IfErrors commit_failed
  ClearErrors
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "DisplayVersion"
  IfErrors commit_failed
  StrCmp $0 "${VERSION}" 0 commit_failed
	RMDir /r /REBOOTOK "$InstallBackup"
	Goto install_done
stage_failed_open:
	FileClose $0
stage_failed:
	SetOutPath "$PLUGINSDIR"
	RMDir /r "$InstallStage"
	!insertmacro Log "event=failure code=I210 phase=stage"
	SetErrorLevel 21
	Abort "Unable to stage the complete Lunitide release; the existing installation was not changed. Diagnostic log: $InstallLog"
swap_failed:
	SetOutPath "$PLUGINSDIR"
	RMDir /r "$InstallStage"
	!insertmacro Log "event=failure code=I300 phase=preserve-existing path=$INSTDIR"
	SetErrorLevel 30
	Abort "Unable to preserve the existing Lunitide release; installation was not continued. Diagnostic log: $InstallLog"
activate_failed:
	SetOutPath "$PLUGINSDIR"
	RMDir /r "$InstallStage"
	!insertmacro Log "event=failure code=I310 phase=activate path=$INSTDIR"
	Goto restore_previous
commit_failed:
  Delete "$DESKTOP\Lunitide.lnk"
  RMDir /r "$SMPROGRAMS\Lunitide"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}"
  RMDir /r "$INSTDIR"
  StrCmp $PreviousInstall 1 restore_previous
  Abort "Unable to commit the new installation; no previous installation was changed."
restore_previous:
	IfFileExists "$InstallBackup\*" 0 restore_failed
	ClearErrors
	Rename "$InstallBackup" "$INSTDIR"
	IfErrors restore_failed
  StrCmp $PreviousRegistration 1 0 restored
  ClearErrors
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "DisplayName" "${PRODUCT}"
  IfErrors restore_metadata_failed
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "DisplayVersion" "$PreviousVersion"
  IfErrors restore_metadata_failed
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "InstallLocation" "$INSTDIR"
  IfErrors restore_metadata_failed
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  IfErrors restore_metadata_failed
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  IfErrors restore_metadata_failed
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "NoModify" 1
  IfErrors restore_metadata_failed
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "NoRepair" 1
  IfErrors restore_metadata_failed
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "DisplayVersion"
  IfErrors restore_metadata_failed
  StrCmp $0 "$PreviousVersion" restored restore_metadata_failed
restored:
	ClearErrors
	CreateDirectory "$SMPROGRAMS\Lunitide"
	IfErrors restore_metadata_failed
	CreateShortcut "$SMPROGRAMS\Lunitide\Lunitide.lnk" "$INSTDIR\Lunitide.exe" "" "$INSTDIR\lunitide-icon.ico" 0
	IfErrors restore_metadata_failed
	CreateShortcut "$DESKTOP\Lunitide.lnk" "$INSTDIR\Lunitide.exe" "" "$INSTDIR\lunitide-icon.ico" 0
	IfErrors restore_metadata_failed
	IfFileExists "$SMPROGRAMS\Lunitide\Lunitide.lnk" restored_start_menu restore_metadata_failed
restored_start_menu:
	IfFileExists "$DESKTOP\Lunitide.lnk" restored_complete restore_metadata_failed
restored_complete:
	!insertmacro Log "event=failure code=I320 phase=commit rollback=complete"
	SetErrorLevel 32
	Abort "Unable to commit the new release; the previous release was restored. Diagnostic log: $InstallLog"
restore_metadata_failed:
	SetErrorLevel 23
	Abort "The previous release files were restored, but its registration or shortcuts could not be restored."
restore_failed:
	SetErrorLevel 22
	Abort "Unable to activate the new release or restore the previous release. The backup was retained."
install_done:
  !insertmacro Log "event=success path=$INSTDIR version=${VERSION}"
SectionEnd
Function un.onInit
  StrCpy $PurgeData 0
  CreateDirectory "$LOCALAPPDATA\LunitideInstaller\Logs"
  StrCpy $InstallLog "$LOCALAPPDATA\LunitideInstaller\Logs\uninstall-latest.log"
  FileOpen $9 "$InstallLog" w
  FileWrite $9 "event=uninstall-start path=$INSTDIR version=${VERSION}$\r$\n"
  FileClose $9
  ${GetParameters} $0
  ${GetOptions} $0 "/PURGE" $1
  ${IfNot} ${Errors}
    StrCpy $PurgeData 1
  ${EndIf}
FunctionEnd
Function un.PurgePage
  nsDialogs::Create 1018
  Pop $0
  ${NSD_CreateCheckbox} 0 20u 100% 20u "Permanently delete local Lunitide data (databases and credentials)"
  Pop $PurgeCheck
  ${If} $PurgeData == 1
    ${NSD_Check} $PurgeCheck
  ${EndIf}
  nsDialogs::Show
FunctionEnd
Function un.PurgeLeave
  ${NSD_GetState} $PurgeCheck $PurgeData
FunctionEnd
Section "Uninstall"
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "InstallLocation"
  StrCmp $0 "$INSTDIR" path_registered
  !insertmacro Log "event=failure code=U100 phase=registration path=$INSTDIR registered=$0"
  IfSilent +2
  MessageBox MB_ICONSTOP|MB_OK "Refusing to uninstall: registration does not match this installation.$\r$\n$\r$\nDiagnostic log: $InstallLog"
  SetErrorLevel 20
  Quit
path_registered:
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$INSTDIR\verify-install-directory.ps1" -Path "$INSTDIR" -MustExist -LogPath "$InstallLog"'
  Pop $0
  StrCmp $0 0 path_ok
  !insertmacro Log "event=failure code=U101 phase=path-validation helper-exit=$0"
  IfSilent +2
  MessageBox MB_ICONSTOP|MB_OK "Refusing to uninstall an unsafe installation path.$\r$\n$\r$\nDiagnostic log: $InstallLog"
  SetErrorLevel 20
  Quit
path_ok:
  ClearErrors
  FileOpen $0 "$INSTDIR\${OWNERFILE}" r
  IfErrors ownership_invalid
  FileRead $0 $1
  FileRead $0 $2
  FileClose $0
  StrCmp $1 "${APPID}" 0 ownership_invalid
  StrCmp $2 "" ownership_ok ownership_invalid
ownership_invalid:
  !insertmacro Log "event=failure code=U110 phase=ownership path=$INSTDIR"
  IfSilent +2
  MessageBox MB_ICONSTOP|MB_OK "Refusing to uninstall: Lunitide ownership marker is missing or invalid.$\r$\n$\r$\nDiagnostic log: $InstallLog"
  SetErrorLevel 20
  Quit
ownership_ok:
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$INSTDIR\stop-install-processes.ps1" -Path "$INSTDIR" -LogPath "$InstallLog"'
  Pop $0
  StrCmp $0 0 +2
  Abort "Unable to stop Lunitide; uninstall was cancelled."
  ${If} $PurgeData == 1
    !insertmacro Log "event=purge-start"
    StrCpy $PurgeAttempts 0
purge_retry:
    nsExec::ExecToLog '"$INSTDIR\purge-user-data.exe"'
    Pop $0
    ${If} $0 != 0
      IntOp $PurgeAttempts $PurgeAttempts + 1
      IntCmp $PurgeAttempts 20 purge_wait purge_wait purge_failed
purge_wait:
      Sleep 250
      Goto purge_retry
purge_failed:
      !insertmacro Log "event=failure code=U120 phase=purge helper-exit=$0"
      Abort "Safe data purge failed; application data was retained."
    ${EndIf}
    !insertmacro Log "event=purge-success"
  ${EndIf}
  Delete "$DESKTOP\Lunitide.lnk"
  RMDir /r "$SMPROGRAMS\Lunitide"
  SetOutPath "$TEMP"
  RMDir /r "$INSTDIR"
  IfFileExists "$INSTDIR\*" 0 uninstall_clean
  !insertmacro Log "event=failure code=U130 phase=remove-install path=$INSTDIR"
  SetErrorLevel 21
  Abort "Unable to remove all Lunitide installation files; uninstall registration was retained for retry."
uninstall_clean:
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}"
  !insertmacro Log "event=uninstall-success path=$INSTDIR"
SectionEnd
