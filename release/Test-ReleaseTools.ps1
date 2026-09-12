#requires -Version 5.1
# Isolated packaging regression: no installer, user registry, signing or app launch.
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot 'Release-Safety.ps1')
$repo=(Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$fixtureParent=[IO.Path]::GetTempPath()
$fixture=Assert-ReleaseChildPath (Join-Path $fixtureParent ('lunitide-release-tools-'+[guid]::NewGuid().ToString('N'))) $fixtureParent
$link=$null
function Expect-Rejected([scriptblock]$Action,[string]$Reason){
  $rejected=$false; try{& $Action | Out-Null}catch{$rejected=$true}
  if(-not $rejected){throw "Expected refusal: $Reason"}
}
try{
  New-Item -Path $fixture -ItemType Directory | Out-Null
  foreach($script in Get-ChildItem -LiteralPath $PSScriptRoot -Filter *.ps1){
    $tokens=$null;$parseErrors=$null
    [void][Management.Automation.Language.Parser]::ParseFile($script.FullName,[ref]$tokens,[ref]$parseErrors)
    if($parseErrors){throw "Invalid script $($script.Name): $parseErrors"}
  }
  Expect-Rejected {Assert-ReleaseChildPath $fixture $fixture} 'root is not its own child'
  Expect-Rejected {Assert-ReleaseChildPath (Join-Path $fixture '..\foreign') $fixture} 'parent traversal'
  $source=Join-Path $fixture 'source'; New-Item $source -ItemType Directory | Out-Null
  & git -C $source init --quiet; if($LASTEXITCODE){throw 'Fixture git init failed'}
  Set-Content -LiteralPath (Join-Path $source 'VERSION') '0.0.1' -Encoding ascii
  Set-Content -LiteralPath (Join-Path $source 'retained.txt') 'retained' -Encoding ascii
  & git -C $source add VERSION retained.txt
  & git -C $source -c user.name=Fixture -c user.email=fixture@example.invalid commit --quiet -m fixture
  if($LASTEXITCODE){throw 'Fixture commit failed'}
  $before=Get-ReleaseSourceSnapshot $source ''
  Assert-ReleaseCandidate $before
  Assert-ReleaseSourceUnchanged $before (Get-ReleaseSourceSnapshot $source '')
  Set-Content -LiteralPath (Join-Path $source 'VERSION') '0.0.2' -Encoding ascii
  Expect-Rejected {Assert-ReleaseSourceUnchanged $before (Get-ReleaseSourceSnapshot $source '')} 'source drift'
  $bad=($before | ConvertTo-Json -Depth 6 | ConvertFrom-Json);$bad.files[0].sha256='a'*64
  Expect-Rejected {Assert-ReleaseCandidate $bad} 'candidate input hash tampering'
  $beforeDeletion=Get-ReleaseSourceSnapshot $source ''
  Remove-Item -LiteralPath (Join-Path $source 'VERSION')
  $withDeletion=Get-ReleaseSourceSnapshot $source ''
  Assert-ReleaseCandidate $withDeletion
  if($withDeletion.fileCount -ne 1){throw 'Deleted tracked input was retained in the actual source snapshot'}
  Expect-Rejected {Assert-ReleaseSourceUnchanged $beforeDeletion $withDeletion} 'source deletion'
  Set-Content -LiteralPath (Join-Path $source 'VERSION') '0.0.2' -Encoding ascii
  Assert-ReleaseSourceUnchanged $beforeDeletion (Get-ReleaseSourceSnapshot $source '')
  $cjkName=(-join @([char]0x4E09,[char]0x573A,[char]0x666F))+'.txt'
  [IO.File]::WriteAllText((Join-Path $source $cjkName),'cjk',(New-Object Text.UTF8Encoding $false))
  $cjkSnap=Get-ReleaseSourceSnapshot $source ''
  Assert-ReleaseCandidate $cjkSnap
  if(@($cjkSnap.files | ForEach-Object {$_.path}) -notcontains $cjkName){throw 'UTF-8 source path was dropped or mojibake'}
  $outside=Join-Path $fixture 'outside';$install=Join-Path $fixture 'install'
  New-Item $outside,$install -ItemType Directory | Out-Null
  Set-Content -LiteralPath (Join-Path $outside 'retain.txt') 'retain'
  $link=Join-Path $install 'linked-data'
  New-Item -ItemType Junction -Path $link -Target $outside | Out-Null
  Expect-Rejected {Assert-NoReleaseReparsePoint $install -Tree} 'nested directory junction'
  & powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot 'verify-install-directory.ps1') -Path $link
  if($LASTEXITCODE -eq 0 -or $LASTEXITCODE -eq 33){throw 'Existing target junction bypassed install validation'}
  & powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot 'verify-install-directory.ps1') -Path $install -MustExist
  if($LASTEXITCODE -eq 0){throw 'Nested junction bypassed uninstall validation'}
  $realData=Join-Path ([Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)) 'Lunitide'
  & powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot 'verify-install-directory.ps1') -Path $realData
  if($LASTEXITCODE -ne 40){throw 'Installation must not overlap the data root'}
  (Get-Item -LiteralPath $link -Force).Delete();$link=$null
  if((Get-Content -LiteralPath (Join-Path $outside 'retain.txt') -Raw).Trim() -cne 'retain'){throw 'Junction fixture changed its target'}
  $build=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'Build-Release.ps1') -Raw
  $layout=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'Verify-Layout.ps1') -Raw
  foreach($name in @('Lunitide.exe','lunitide-engine.exe','lunitide-maintenance.exe','purge-user-data.exe')){
    if(-not $build.Contains($name) -or -not $layout.Contains($name)){throw "Incomplete executable inventory: $name"}
  }
  $engine=Get-Content -LiteralPath (Join-Path $repo 'cmd\engine\main.go') -Raw
  if($engine.IndexOf('doctext.RunWorker') -lt 0 -or $engine.IndexOf('doctext.RunWorker') -gt $engine.IndexOf('ipc.ReadLaunchBootstrap')){throw 'Parser worker must exit before bootstrap and credential access'}
  $oldCgo=$env:CGO_ENABLED
  try{
    $env:CGO_ENABLED='0'
    Push-Location $repo
    try{& go build -trimpath -o (Join-Path $fixture 'lunitide-maintenance.exe') ./cmd/maintenance;if($LASTEXITCODE){throw 'Maintenance artifact build failed'}}finally{Pop-Location}
    & (Join-Path $PSScriptRoot 'Verify-PE.ps1') (Join-Path $fixture 'lunitide-maintenance.exe')
    # -h exits during flag parsing, before preparing the production data root.
    $help=Start-Process -WindowStyle Hidden (Join-Path $fixture 'lunitide-maintenance.exe') -ArgumentList '-h' -Wait -PassThru -RedirectStandardError (Join-Path $fixture 'help.txt')
    try{if($help.ExitCode){throw 'Maintenance artifact help failed'}}finally{$help.Dispose()}
  }finally{$env:CGO_ENABLED=$oldCgo}
  Write-Host 'Release tools: source drift/hash, path boundaries/junctions, scripts, worker inventory, maintenance PE/help passed'
}finally{
  if($link -and (Test-Path -LiteralPath $link)){(Get-Item -LiteralPath $link -Force).Delete()}
  $null=Assert-ReleaseChildPath $fixture $fixtureParent
  Remove-Item -LiteralPath $fixture -Recurse -Force -ErrorAction SilentlyContinue
}
