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
!insertmacro GetSize
!define APPID "Lunitide.Desktop.7A565D82-936E-4E06-962D-83B5DD24E53C"
!define OWNERFILE ".lunitide-install-owner"
!define PRODUCT "Lunitide"
!define PUBLISHER "Yy.MJ"
; ADR-003: v1 ships against the Evergreen WebView2 Runtime. The installer MUST
; detect a minimum runtime version and offer recoverable acquisition guidance.
; The minimum tracks the pinned loader generation (1.0.3537.50 SDK ~ runtime
; 134); anything older cannot be assumed to implement the APIs in use.
!define WV2CLIENT "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
!define WV2MINVERSION "134.0.0.0"
!define WV2URL "https://developer.microsoft.com/microsoft-edge/webview2/#download-section"
Name "${PRODUCT}"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\Lunitide"
InstallDirRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "InstallLocation"
SetCompressor /SOLID lzma
VIProductVersion "${VERSION}.0"
VIAddVersionKey "CompanyName" "${PUBLISHER}"
VIAddVersionKey "FileDescription" "${PRODUCT}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductName" "${PRODUCT}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "${PUBLISHER}"
!define MUI_ABORTWARNING
!define MUI_ICON "${STAGE}\lunitide-icon.ico"
!define MUI_UNICON "${STAGE}\lunitide-icon.ico"
!macro WriteUninstallRegistration Ver
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "DisplayName" "${PRODUCT}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "Publisher" "${PUBLISHER}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "DisplayVersion" "${Ver}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "DisplayIcon" "$INSTDIR\lunitide-icon.ico"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $R0 $R1 $R2
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}" "EstimatedSize" $R0
!macroend
!insertmacro MUI_PAGE_WELCOME
Page custom WV2GuideShow WV2GuideLeave
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
Var WV2Version
Var WV2Continue
Var WV2BtnDownload
Var WV2BtnRetry
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
  StrCpy $WV2Continue 0
  Call DetectWebView2
  ${If} $WV2Version == ""
    !insertmacro Log "event=warning code=W140 phase=webview2 reason=runtime-absent-or-too-old min=${WV2MINVERSION}"
  ${Else}
    !insertmacro Log "event=webview2 version=$WV2Version"
  ${EndIf}
FunctionEnd
Function DetectWebView2
  ; Evergreen Runtime registration: per-user under HKCU, per-machine under the
  ; 32-bit HKLM view (EdgeUpdate is x86); the 64-bit view is a fallback.
  StrCpy $WV2Version ""
  ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\${WV2CLIENT}" "pv"
  StrCmp $0 "" +2 0
    StrCpy $WV2Version $0
  ReadRegStr $0 HKLM "Software\Microsoft\EdgeUpdate\Clients\${WV2CLIENT}" "pv"
  StrCmp $0 "" +2 0
    StrCpy $WV2Version $0
  SetRegView 64
  ReadRegStr $0 HKLM "Software\Microsoft\EdgeUpdate\Clients\${WV2CLIENT}" "pv"
  SetRegView 32
  StrCmp $0 "" +2 0
    StrCpy $WV2Version $0
  ${If} $WV2Version != ""
    ${VersionCompare} $WV2Version "${WV2MINVERSION}" $0
    ${If} $0 == 2
      !insertmacro Log "event=warning code=W141 phase=webview2 reason=runtime-too-old found=$WV2Version min=${WV2MINVERSION}"
      StrCpy $WV2Version ""
    ${EndIf}
  ${EndIf}
FunctionEnd
Function WV2OpenDownload
  ExecShell "open" "${WV2URL}"
FunctionEnd
Function WV2Recheck
  Call DetectWebView2
  ${If} $WV2Version != ""
    MessageBox MB_ICONINFORMATION|MB_OK "WebView2 Runtime detected (version $WV2Version). Click Next to continue."
  ${EndIf}
FunctionEnd
Function WV2GuideShow
  ${If} ${Silent}
    Abort
  ${EndIf}
  ${If} $WV2Version != ""
    Abort
  ${EndIf}
  nsDialogs::Create 1018
  Pop $0
  ${NSD_CreateLabel} 0 0 100% 40u "Lunitide needs the Microsoft Edge WebView2 Runtime (${WV2MINVERSION} or later). None was detected. Install Microsoft's official Evergreen runtime, then click Recheck. You can continue without it, but Lunitide will not start until the runtime is installed."
  Pop $0
  ${NSD_CreateButton} 0 46u 48% 14u "Open runtime download page"
  Pop $WV2BtnDownload
  ${NSD_OnClick} $WV2BtnDownload WV2OpenDownload
  ${NSD_CreateButton} 52% 46u 48% 14u "Recheck"
  Pop $WV2BtnRetry
  ${NSD_OnClick} $WV2BtnRetry WV2Recheck
  nsDialogs::Show
FunctionEnd
Function WV2GuideLeave
  Call DetectWebView2
  ${If} $WV2Version == ""
    ${If} $WV2Continue != 1
      MessageBox MB_ICONEXCLAMATION|MB_YESNO "WebView2 Runtime is still missing. Lunitide will not start until it is installed.$\r$\n$\r$\nContinue the installation anyway?" IDYES wv2_continue_install
      Abort
wv2_continue_install:
      StrCpy $WV2Continue 1
      !insertmacro Log "event=warning code=W142 phase=webview2 reason=user-continued-without-runtime"
    ${EndIf}
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
  Abort "An existing Lunitide install must be upgraded in place. Uninstall first to change the location. Log: $InstallLog"
invalid_install_path:
  !insertmacro Log "event=failure code=I100 phase=target reason=invalid-path path=$INSTDIR"
  SetErrorLevel 10
  Abort "Choose a folder on a local fixed drive, for example D:\Apps\Lunitide. Log: $InstallLog"
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
  Abort "The selected folder is not empty and is not a Lunitide install. Choose an empty folder. Log: $InstallLog"
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
  Abort "Downgrade refused. Re-run with /ALLOWDOWNGRADE if you really need an older version. Log: $InstallLog"
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
  Abort "Could not stop a running Lunitide process. Close Lunitide and retry. Log: $InstallLog"
unsafe_path:
  !insertmacro Log "event=failure code=I130 phase=path-validation helper-exit=$0"
  SetErrorLevel 13
  Abort "The selected install path or a temporary replace path is not safe. Log: $InstallLog"
parent_failed:
  !insertmacro Log "event=failure code=I131 phase=create-parent path=$InstallParent"
  SetErrorLevel 13
  Abort "Could not create the selected install folder. Check that the disk is writable. Log: $InstallLog"
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
  !insertmacro WriteUninstallRegistration "${VERSION}"
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
	Abort "Could not stage a complete Lunitide release. The existing install was not changed. Log: $InstallLog"
swap_failed:
	SetOutPath "$PLUGINSDIR"
	RMDir /r "$InstallStage"
	!insertmacro Log "event=failure code=I300 phase=preserve-existing path=$INSTDIR"
	SetErrorLevel 30
	Abort "Could not preserve the existing Lunitide install. Setup was aborted. Log: $InstallLog"
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
  Abort "Could not commit the new install. No previous install was changed."
restore_previous:
	IfFileExists "$InstallBackup\*" 0 restore_failed
	ClearErrors
	Rename "$InstallBackup" "$INSTDIR"
	IfErrors restore_failed
  StrCmp $PreviousRegistration 1 0 restored
  ClearErrors
  !insertmacro WriteUninstallRegistration "$PreviousVersion"
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
	Abort "Could not commit the new version. The previous install was restored. Log: $InstallLog"
restore_metadata_failed:
	SetErrorLevel 23
	Abort "Previous files were restored, but registration or shortcuts could not be restored."
restore_failed:
	SetErrorLevel 22
	Abort "Could not enable the new version or restore the previous version. The backup was kept."
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
  ${NSD_CreateCheckbox} 0 20u 100% 20u "Permanently delete local Lunitide data (database and credentials)"
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
  MessageBox MB_ICONSTOP|MB_OK "Uninstall refused: registration does not match this install.$\r$\n$\r$\nLog: $InstallLog"
  SetErrorLevel 20
  Quit
path_registered:
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$INSTDIR\verify-install-directory.ps1" -Path "$INSTDIR" -MustExist -LogPath "$InstallLog"'
  Pop $0
  StrCmp $0 0 path_ok
  !insertmacro Log "event=failure code=U101 phase=path-validation helper-exit=$0"
  IfSilent +2
  MessageBox MB_ICONSTOP|MB_OK "Install path is not safe. Uninstall refused.$\r$\n$\r$\nLog: $InstallLog"
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
  MessageBox MB_ICONSTOP|MB_OK "Uninstall refused: Lunitide ownership mark is missing or invalid.$\r$\n$\r$\nLog: $InstallLog"
  SetErrorLevel 20
  Quit
ownership_ok:
  nsExec::ExecToLog 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$INSTDIR\stop-install-processes.ps1" -Path "$INSTDIR" -LogPath "$InstallLog"'
  Pop $0
  StrCmp $0 0 +2
  Abort "Could not stop Lunitide. Uninstall cancelled."
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
      Abort "Secure data purge failed. Application data was kept."
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
  Abort "Could not remove all Lunitide files. Uninstall registration was kept so you can retry."
uninstall_clean:
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}"
  !insertmacro Log "event=uninstall-success path=$INSTDIR"
SectionEnd
