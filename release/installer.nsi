Unicode true
RequestExecutionLevel user
!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "nsDialogs.nsh"
!include "WordFunc.nsh"
!insertmacro VersionCompare
!insertmacro GetFileName
!define APPID "Lunitide.Desktop.7A565D82-936E-4E06-962D-83B5DD24E53C"
!define OWNERFILE ".lunitide-install-owner"
!define PRODUCT "Lunitide 月汐"
Name "${PRODUCT}"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\Lunitide"
SetCompressor /SOLID lzma
!define MUI_ABORTWARNING
!insertmacro MUI_PAGE_WELCOME
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
Function .onInit
  StrCpy $AllowDowngrade 0
  StrCpy $PreviousRegistration 0
  StrCpy $PreviousVersion ""
  StrCpy $PreviousInstall 0
  ${GetParameters} $0
  ${GetOptions} $0 "/ALLOWDOWNGRADE" $1
  ${IfNot} ${Errors}
    StrCpy $AllowDowngrade 1
  ${EndIf}
FunctionEnd
Section "Install"
  StrCpy $INSTDIR "$LOCALAPPDATA\Programs\Lunitide"
  IfFileExists "$INSTDIR\*" 0 ownership_ok
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
  Abort "Refusing to replace a non-empty directory not owned by Lunitide."
ownership_version:
  ReadRegStr $1 HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "DisplayVersion"
  StrCmp $1 "" ownership_ok
  StrCpy $PreviousRegistration 1
  StrCpy $PreviousVersion $1
  ${VersionCompare} $1 "${VERSION}" $2
  StrCmp $2 1 0 ownership_ok
  StrCmp $AllowDowngrade 1 ownership_ok
  Abort "Downgrade refused. Re-run with /ALLOWDOWNGRADE to explicitly permit it."
ownership_ok:
  InitPluginsDir
  ClearErrors
  CreateDirectory "$LOCALAPPDATA\Programs"
  IfErrors parent_failed
  SetOutPath "$PLUGINSDIR"
  File /oname=stop-install-processes.ps1 "${STAGE}\stop-install-processes.ps1"
  File /oname=verify-install-directory.ps1 "${STAGE}\verify-install-directory.ps1"
  ${GetFileName} "$PLUGINSDIR" $1
  StrCpy $InstallStage "$LOCALAPPDATA\Programs\Lunitide.installing.$1"
  StrCpy $InstallBackup "$LOCALAPPDATA\Programs\Lunitide.backup.$1"
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\verify-install-directory.ps1" -Path "$InstallStage"'
  Pop $0
  StrCmp $0 0 +2
  Abort "The Lunitide staging path is not safe."
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\verify-install-directory.ps1" -Path "$InstallBackup"'
  Pop $0
  StrCmp $0 0 +2
  Abort "The Lunitide backup path is not safe."
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\stop-install-processes.ps1"'
  Pop $0
  StrCmp $0 0 stop_ok
  Abort "Unable to stop the existing Lunitide processes; no files were replaced."
parent_failed:
  Abort "Unable to prepare the Lunitide installation parent directory."
stop_ok:
  ClearErrors
  CreateDirectory "$InstallStage"
  IfErrors stage_failed
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\verify-install-directory.ps1" -Path "$InstallStage" -MustExist'
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
	nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\verify-install-directory.ps1" -Path "$InstallStage" -MustExist'
	Pop $0
	StrCmp $0 0 +2
	Goto stage_failed
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
  CreateShortcut "$SMPROGRAMS\Lunitide\Lunitide.lnk" "$INSTDIR\Lunitide.exe"
  IfErrors commit_failed
  ClearErrors
  CreateShortcut "$DESKTOP\Lunitide.lnk" "$INSTDIR\Lunitide.exe"
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
	RMDir /r "$InstallStage"
	Abort "Unable to stage the complete Lunitide release; the existing installation was not changed."
swap_failed:
	RMDir /r "$InstallStage"
	Abort "Unable to preserve the existing Lunitide release; installation was not continued."
activate_failed:
	RMDir /r "$InstallStage"
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
	CreateShortcut "$SMPROGRAMS\Lunitide\Lunitide.lnk" "$INSTDIR\Lunitide.exe"
	IfErrors restore_metadata_failed
	CreateShortcut "$DESKTOP\Lunitide.lnk" "$INSTDIR\Lunitide.exe"
	IfErrors restore_metadata_failed
	IfFileExists "$SMPROGRAMS\Lunitide\Lunitide.lnk" restored_start_menu restore_metadata_failed
restored_start_menu:
	IfFileExists "$DESKTOP\Lunitide.lnk" restored_complete restore_metadata_failed
restored_complete:
	Abort "Unable to commit the new release; the previous release was restored."
restore_metadata_failed:
	SetErrorLevel 23
	Abort "The previous release files were restored, but its registration or shortcuts could not be restored."
restore_failed:
	SetErrorLevel 22
	Abort "Unable to activate the new release or restore the previous release. The backup was retained."
install_done:
SectionEnd
Function un.onInit
  StrCpy $PurgeData 0
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
  StrCmp $INSTDIR "$LOCALAPPDATA\Programs\Lunitide" path_ok
  Abort "Refusing to uninstall: installation path is not the fixed Lunitide path."
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
  SetErrorLevel 20
  Abort "Refusing to uninstall: Lunitide ownership marker is missing or invalid."
ownership_ok:
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$INSTDIR\stop-install-processes.ps1"'
  Pop $0
  StrCmp $0 0 +2
  Abort "Unable to stop Lunitide; uninstall was cancelled."
  ${If} $PurgeData == 1
    nsExec::ExecToLog '"$INSTDIR\purge-user-data.exe"'
    Pop $0
    ${If} $0 != 0
      Abort "Safe data purge failed; application data was retained."
    ${EndIf}
  ${EndIf}
  Delete "$DESKTOP\Lunitide.lnk"
  RMDir /r "$SMPROGRAMS\Lunitide"
  RMDir /r "$INSTDIR"
  IfFileExists "$INSTDIR\*" 0 uninstall_clean
  SetErrorLevel 21
  Abort "Unable to remove all Lunitide installation files; uninstall registration was retained for retry."
uninstall_clean:
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}"
SectionEnd
