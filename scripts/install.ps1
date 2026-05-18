[CmdletBinding()]
param(
  [string]$Binary,
  [string]$InstallDir,
  [string]$Version,
  [string]$Repo,
  [string]$BaseUrl,
  [string]$Lang,
  [string]$OnboardMode,
  [switch]$RunOnboard,
  [switch]$Help
)

$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$supportedBinaries = @('hopclaw', 'openclaw', 'hopclaw-browserd', 'hopclaw-desktopd', 'hopclaw-gateway')
$script:InstallLang = 'en'
$script:Headers = @{
  'User-Agent' = 'HopClaw-Installer'
  'Accept' = 'application/vnd.github+json'
}

function Write-Usage {
  @'
Install HopClaw release binaries from the hosted release surface on Windows.

Parameters:
  -Binary        Binary or comma-separated binaries to install.
                 Supported: hopclaw, openclaw, hopclaw-browserd,
                 hopclaw-desktopd, hopclaw-gateway, or all
  -InstallDir    Destination directory. Default:
                 %LOCALAPPDATA%\Programs\HopClaw\bin
  -Version       Release tag or "latest". Default: latest
  -Repo          Override the GitHub repo slug, e.g. hopclaw/hopclaw
                 When set, the installer downloads from GitHub releases
  -BaseUrl       Hosted release base URL. Default:
                 https://hopclaw.com/releases
  -Lang          Installer/onboarding language: en or zh
  -OnboardMode   Onboarding handoff mode when -RunOnboard is used:
                 interactive (default) or web-first
  -RunOnboard    Run "hopclaw onboard" after installing a CLI binary
  -Help          Show this message

Environment variables:
  HOPCLAW_INSTALL_BINARY
  HOPCLAW_INSTALL_DIR
  HOPCLAW_INSTALL_VERSION
  HOPCLAW_INSTALL_REPO
  HOPCLAW_INSTALL_BASE_URL
  HOPCLAW_INSTALL_LANG
  HOPCLAW_INSTALL_RUN_ONBOARD
  HOPCLAW_INSTALL_ONBOARD_MODE

Examples:
  irm https://hopclaw.com/install.ps1 | iex
  $env:HOPCLAW_INSTALL_LANG = 'zh'; irm https://hopclaw.com/install.ps1 | iex
  $env:HOPCLAW_INSTALL_RUN_ONBOARD = '1'; irm https://hopclaw.com/install.ps1 | iex
  $env:HOPCLAW_INSTALL_RUN_ONBOARD = '1'; $env:HOPCLAW_INSTALL_ONBOARD_MODE = 'web-first'; irm https://hopclaw.com/install.ps1 | iex
  $env:HOPCLAW_INSTALL_BINARY = 'all'; irm https://hopclaw.com/install.ps1 | iex
  $env:HOPCLAW_INSTALL_BASE_URL = 'https://mirror.example.com/releases'; irm https://hopclaw.com/install.ps1 | iex
  powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1 -RunOnboard
'@ | Write-Host
}

function T {
  param(
    [string]$English,
    [string]$Chinese
  )

  if ($script:InstallLang -eq 'zh' -and -not [string]::IsNullOrWhiteSpace($Chinese)) {
    return $Chinese
  }
  return $English
}

function Get-EnvOrDefault {
  param(
    [string]$Name,
    [string]$DefaultValue
  )

  $value = [Environment]::GetEnvironmentVariable($Name)
  if ([string]::IsNullOrWhiteSpace($value)) {
    return $DefaultValue
  }
  return $value.Trim()
}

function Test-Truthy {
  param([string]$Value)

  if ([string]::IsNullOrWhiteSpace($Value)) {
    return $false
  }

  switch ($Value.Trim().ToLowerInvariant()) {
    '1' { return $true }
    'y' { return $true }
    'yes' { return $true }
    'true' { return $true }
    'on' { return $true }
    default { return $false }
  }
}

function Get-DefaultInstallDir {
  $localAppData = [Environment]::GetFolderPath('LocalApplicationData')
  if ([string]::IsNullOrWhiteSpace($localAppData)) {
    throw 'failed to resolve LocalApplicationData'
  }
  return [IO.Path]::Combine($localAppData, 'Programs', 'HopClaw', 'bin')
}

function Resolve-Repo {
  if ([string]::IsNullOrWhiteSpace($Repo)) {
    throw 'HOPCLAW_INSTALL_REPO cannot be empty when -Repo is used'
  }
  return $Repo.Trim()
}

function Resolve-BaseUrl {
  param([string]$Value)

  if ([string]::IsNullOrWhiteSpace($Value)) {
    return 'https://hopclaw.com/releases'
  }
  return $Value.Trim().TrimEnd('/')
}

function Normalize-InstallLanguage {
  param([string]$Value)

  if ([string]::IsNullOrWhiteSpace($Value)) {
    return ''
  }

  $normalized = $Value.Trim().ToLowerInvariant().Replace('_', '-')
  if ($normalized -eq 'zh' -or $normalized.StartsWith('zh-') -or $normalized -eq 'cn' -or $normalized -eq 'chinese') {
    return 'zh'
  }
  if ($normalized -eq 'en' -or $normalized.StartsWith('en-') -or $normalized -eq 'english') {
    return 'en'
  }
  return ''
}

function Get-DefaultInstallLanguage {
  foreach ($candidate in @($env:LC_ALL, $env:LC_MESSAGES, $env:LANG)) {
    $normalized = Normalize-InstallLanguage -Value $candidate
    if (-not [string]::IsNullOrWhiteSpace($normalized)) {
      return $normalized
    }
  }
  return 'en'
}

function Resolve-InstallLanguage {
  param([string]$Value)

  $normalized = Normalize-InstallLanguage -Value $Value
  if (-not [string]::IsNullOrWhiteSpace($normalized)) {
    return $normalized
  }
  if (-not [string]::IsNullOrWhiteSpace($Value)) {
    throw "unsupported installer language: $Value (use en or zh)"
  }

  $default = Get-DefaultInstallLanguage
  if (-not [Environment]::UserInteractive) {
    return $default
  }

  Write-Host ''
  Write-Host 'Choose installer language / 选择安装语言'
  if ($default -eq 'zh') {
    Write-Host '  1) 中文（默认）'
    Write-Host '  2) English'
  } else {
    Write-Host '  1) 中文'
    Write-Host '  2) English (default)'
  }
  $choice = (Read-Host '> ').Trim().ToLowerInvariant()
  switch ($choice) {
    '' { return $default }
    '1' { return 'zh' }
    'zh' { return 'zh' }
    'cn' { return 'zh' }
    '中文' { return 'zh' }
    '2' { return 'en' }
    'en' { return 'en' }
    'english' { return 'en' }
    default { return $default }
  }
}

function Resolve-OnboardMode {
  param([string]$Value)

  if ([string]::IsNullOrWhiteSpace($Value)) {
    return 'interactive'
  }

  $normalized = $Value.Trim().ToLowerInvariant()
  if ($normalized -notin @('interactive', 'web-first')) {
    throw "unsupported onboarding mode: $Value (use interactive or web-first)"
  }
  return $normalized
}

function Resolve-Arch {
  $candidates = @($env:PROCESSOR_ARCHITEW6432, $env:PROCESSOR_ARCHITECTURE)
  foreach ($candidate in $candidates) {
    if ([string]::IsNullOrWhiteSpace($candidate)) {
      continue
    }
    switch ($candidate.Trim().ToUpperInvariant()) {
      'AMD64' { return 'amd64' }
      'ARM64' { return 'arm64' }
    }
  }
  throw "unsupported architecture: $($env:PROCESSOR_ARCHITECTURE)"
}

function Resolve-Binaries {
  param([string]$InputValue)

  if ([string]::IsNullOrWhiteSpace($InputValue)) {
    throw 'HOPCLAW_INSTALL_BINARY cannot be empty'
  }

  $normalized = $InputValue.Trim()
  if ($normalized -in @('-h', '--help')) {
    Write-Usage
    exit 0
  }

  if ($normalized.ToLowerInvariant() -eq 'all') {
    return $supportedBinaries
  }

  $resolved = New-Object System.Collections.Generic.List[string]
  foreach ($item in ($normalized -split '[,\s]+' | Where-Object { $_ })) {
    $binaryName = $item.Trim()
    if ($supportedBinaries -notcontains $binaryName) {
      throw "unsupported binary: $binaryName"
    }
    if (-not $resolved.Contains($binaryName)) {
      [void]$resolved.Add($binaryName)
    }
  }

  if ($resolved.Count -eq 0) {
    throw 'HOPCLAW_INSTALL_BINARY cannot be empty'
  }

  return $resolved.ToArray()
}

function Resolve-LatestTag {
  param([string]$Repository)

  $release = Invoke-RestMethod -Headers $script:Headers -Uri "https://api.github.com/repos/$Repository/releases/latest"
  if ([string]::IsNullOrWhiteSpace($release.tag_name)) {
    throw "failed to resolve the latest release tag for $Repository"
  }
  return [string]$release.tag_name
}

function Resolve-LatestHostedVersion {
  param([string]$ReleaseBaseUrl)

  $latestUrl = "$ReleaseBaseUrl/LATEST"
  $response = Invoke-WebRequest -Uri $latestUrl
  $latest = [string]$response.Content
  $latest = ($latest -split "[`r`n]")[0].Trim()
  $latest = $latest.TrimStart('v', 'V')
  if ([string]::IsNullOrWhiteSpace($latest)) {
    throw "failed to resolve the latest hosted release version from $latestUrl"
  }
  return $latest
}

function Download-File {
  param(
    [string]$Url,
    [string]$Destination
  )

  Invoke-WebRequest -Uri $Url -OutFile $Destination
}

function Verify-ArchiveChecksum {
  param(
    [string]$ChecksumsUrl,
    [string]$Archive,
    [string]$ArchivePath,
    [string]$TempDir
  )

  $checksumsPath = Join-Path $TempDir 'checksums.txt'

  try {
    Download-File -Url $ChecksumsUrl -Destination $checksumsPath
  } catch {
    Write-Warning "checksum manifest not found at $ChecksumsUrl; continuing without verification"
    return
  }

  $expected = $null
  foreach ($line in Get-Content -Path $checksumsPath) {
    if ($line -match "^\s*([a-fA-F0-9]{64})\s+\*?(.+?)\s*$") {
      if ($matches[2] -eq $Archive) {
        $expected = $matches[1].ToLowerInvariant()
        break
      }
    }
  }

  if ([string]::IsNullOrWhiteSpace($expected)) {
    Write-Warning "checksum for $Archive not found; continuing without verification"
    return
  }

  $actual = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $expected) {
    throw "checksum verification failed for $Archive"
  }
}

function Get-BinaryFileName {
  param([string]$BinaryName)
  return "$BinaryName.exe"
}

function Test-PathEntry {
  param(
    [string]$PathValue,
    [string]$Target
  )

  if ([string]::IsNullOrWhiteSpace($PathValue)) {
    return $false
  }

  $normalizedTarget = $Target.TrimEnd('\')
  foreach ($entry in ($PathValue -split ';' | Where-Object { $_ })) {
    if ($entry.TrimEnd('\').Equals($normalizedTarget, [StringComparison]::OrdinalIgnoreCase)) {
      return $true
    }
  }

  return $false
}

function Ensure-InstallDirOnPath {
  param([string]$TargetDir)

  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  $added = $false
  if (-not (Test-PathEntry -PathValue $userPath -Target $TargetDir)) {
    $newUserPath = if ([string]::IsNullOrWhiteSpace($userPath)) {
      $TargetDir
    } else {
      "$userPath;$TargetDir"
    }
    [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
    $added = $true
  }

  if (-not (Test-PathEntry -PathValue $env:Path -Target $TargetDir)) {
    $env:Path = "$TargetDir;$env:Path"
  }

  return $added
}

function Write-NextSteps {
  param(
    [string]$PrimaryCli,
    [string]$TargetDir,
    [bool]$PathAdded
  )

  if ([string]::IsNullOrWhiteSpace($PrimaryCli)) {
    return
  }

  Write-Host ''
  Write-Host (T 'next steps:' '接下来可以这样做：')
  Write-Host (T "  $PrimaryCli onboard     # full interactive setup" "  $PrimaryCli onboard     # 继续安装向导")
  Write-Host (T "  $PrimaryCli dashboard   # open the local dashboard" "  $PrimaryCli dashboard   # 查看本地控制台地址")
  Write-Host (T "  $PrimaryCli update      # check for newer releases" "  $PrimaryCli update      # 检查新版本")

  if ($PathAdded) {
    Write-Host ''
    Write-Host (T "note: added $TargetDir to your user PATH" "注意：已将 $TargetDir 添加到你的用户 PATH")
    Write-Host (T 'open a new PowerShell window if the command is not visible yet' '如果当前窗口还看不到命令，请重新打开一个 PowerShell 窗口。')
  }
}

function Write-Step {
  param(
    [string]$Message,
    [int]$Percent = -1
  )

  if ($Percent -ge 0) {
    Write-Progress -Activity (T 'Installing HopClaw' '正在安装 HopClaw') -Status $Message -PercentComplete $Percent
  }
  Write-Host "==> $Message"
}

function Write-Detail {
  param([string]$Message)
  Write-Host "   $Message"
}

if ($Help -or (Get-EnvOrDefault -Name 'HOPCLAW_INSTALL_BINARY' -DefaultValue '') -in @('-h', '--help')) {
  Write-Usage
  exit 0
}

if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
  throw 'scripts/install.ps1 is intended for Windows'
}

if ([string]::IsNullOrWhiteSpace($Binary)) {
  $Binary = Get-EnvOrDefault -Name 'HOPCLAW_INSTALL_BINARY' -DefaultValue 'hopclaw'
}
if ([string]::IsNullOrWhiteSpace($InstallDir)) {
  $InstallDir = Get-EnvOrDefault -Name 'HOPCLAW_INSTALL_DIR' -DefaultValue (Get-DefaultInstallDir)
}
if ([string]::IsNullOrWhiteSpace($Version)) {
  $Version = Get-EnvOrDefault -Name 'HOPCLAW_INSTALL_VERSION' -DefaultValue 'latest'
}
if ([string]::IsNullOrWhiteSpace($Repo)) {
  $Repo = Get-EnvOrDefault -Name 'HOPCLAW_INSTALL_REPO' -DefaultValue ''
}
if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
  $BaseUrl = Get-EnvOrDefault -Name 'HOPCLAW_INSTALL_BASE_URL' -DefaultValue 'https://hopclaw.com/releases'
}
if ([string]::IsNullOrWhiteSpace($Lang)) {
  $Lang = Get-EnvOrDefault -Name 'HOPCLAW_INSTALL_LANG' -DefaultValue ''
}
if ([string]::IsNullOrWhiteSpace($OnboardMode)) {
  $OnboardMode = Get-EnvOrDefault -Name 'HOPCLAW_INSTALL_ONBOARD_MODE' -DefaultValue 'interactive'
}
$script:InstallLang = Resolve-InstallLanguage -Value $Lang
$env:HOPCLAW_INSTALL_LANG = $script:InstallLang
$OnboardMode = Resolve-OnboardMode -Value $OnboardMode

$runOnboardRequested = $RunOnboard.IsPresent
if (-not $runOnboardRequested) {
  $runOnboardRequested = Test-Truthy (Get-EnvOrDefault -Name 'HOPCLAW_INSTALL_RUN_ONBOARD' -DefaultValue '0')
}

$sourceMode = if ([string]::IsNullOrWhiteSpace($Repo)) { 'hosted' } else { 'github' }
$repoSlug = $null
$releaseBaseUrl = Resolve-BaseUrl -Value $BaseUrl
if ($sourceMode -eq 'github') {
  $repoSlug = Resolve-Repo
}
$binaries = Resolve-Binaries -InputValue $Binary
$arch = Resolve-Arch
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ("hopclaw-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempDir | Out-Null

Write-Step -Message (T 'Starting HopClaw installer' '开始安装 HopClaw') -Percent 2
Write-Detail (T "target: windows/$arch" "目标：windows/$arch")
Write-Detail (T "install dir: $InstallDir" "安装目录：$InstallDir")

try {
  $archivePath = $null
  $tag = $null
  $archiveName = $null
  $installSource = $null

  if ($sourceMode -eq 'github') {
    Write-Detail (T "source: github.com/$repoSlug/releases" "来源：github.com/$repoSlug/releases")
    $tagCandidates = New-Object System.Collections.Generic.List[string]
    if ($Version.Trim().ToLowerInvariant() -eq 'latest') {
      Write-Step -Message (T "Resolving latest GitHub release for $repoSlug" "解析 $repoSlug 的最新 GitHub 版本") -Percent 10
      [void]$tagCandidates.Add((Resolve-LatestTag -Repository $repoSlug))
    } else {
      $requested = $Version.Trim()
      [void]$tagCandidates.Add($requested)
      if (-not $requested.StartsWith('v', [StringComparison]::OrdinalIgnoreCase)) {
        [void]$tagCandidates.Add("v$requested")
      }
    }

    foreach ($candidate in $tagCandidates) {
      $versionPart = $candidate.TrimStart('v', 'V')
      $archiveName = "hopclaw_${versionPart}_windows_${arch}.zip"
      $candidatePath = Join-Path $tempDir $archiveName
      $downloadUrl = "https://github.com/$repoSlug/releases/download/$candidate/$archiveName"
      Write-Step -Message (T "Downloading $archiveName" "下载 $archiveName") -Percent 30
      try {
        Download-File -Url $downloadUrl -Destination $candidatePath
        $archivePath = $candidatePath
        $tag = $candidate
        $installSource = "$repoSlug release $tag"
        break
      } catch {
        if (Test-Path -Path $candidatePath) {
          Remove-Item -Path $candidatePath -Force -ErrorAction SilentlyContinue
        }
      }
    }

    if ([string]::IsNullOrWhiteSpace($archivePath) -or [string]::IsNullOrWhiteSpace($tag)) {
      throw "failed to download a matching release archive for $repoSlug (windows/$arch)"
    }

    Write-Step -Message (T "Verifying checksum for $archiveName" "校验 $archiveName 的校验和") -Percent 50
    Verify-ArchiveChecksum -ChecksumsUrl "https://github.com/$repoSlug/releases/download/$tag/checksums.txt" -Archive $archiveName -ArchivePath $archivePath -TempDir $tempDir
  } else {
    Write-Detail (T "source: $releaseBaseUrl" "来源：$releaseBaseUrl")
    $resolvedVersion = if ($Version.Trim().ToLowerInvariant() -eq 'latest') {
      Write-Step -Message (T 'Resolving latest hosted release version' '解析最新托管版本') -Percent 10
      Resolve-LatestHostedVersion -ReleaseBaseUrl $releaseBaseUrl
    } else {
      $Version.Trim().TrimStart('v', 'V')
    }
    $archiveName = "hopclaw_${resolvedVersion}_windows_${arch}.zip"
    $archivePath = Join-Path $tempDir $archiveName
    $tag = $resolvedVersion
    $downloadUrl = "$releaseBaseUrl/download/$resolvedVersion/$archiveName"
    Write-Step -Message (T "Downloading $archiveName" "下载 $archiveName") -Percent 30
    try {
      Download-File -Url $downloadUrl -Destination $archivePath
    } catch {
      throw "failed to download a matching release archive from $releaseBaseUrl (windows/$arch, version $resolvedVersion)"
    }

    $installSource = "$releaseBaseUrl release $tag"
    Write-Step -Message (T "Verifying checksum for $archiveName" "校验 $archiveName 的校验和") -Percent 50
    Verify-ArchiveChecksum -ChecksumsUrl "$releaseBaseUrl/download/$resolvedVersion/checksums.txt" -Archive $archiveName -ArchivePath $archivePath -TempDir $tempDir
  }

  $extractDir = Join-Path $tempDir 'extract'
  Write-Step -Message (T "Extracting $archiveName" "解压 $archiveName") -Percent 65
  Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force
  New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

  $primaryCliBinary = $null
  $primaryCliPath = $null
  Write-Step -Message (T "Installing binaries into $InstallDir" "安装二进制到 $InstallDir") -Percent 80
  foreach ($binaryName in $binaries) {
    $fileName = Get-BinaryFileName -BinaryName $binaryName
    $sourceFile = Get-ChildItem -Path $extractDir -Recurse -File -Filter $fileName | Select-Object -First 1
    if (-not $sourceFile) {
      throw "binary $fileName was not found in the downloaded archive"
    }

    $destination = Join-Path $InstallDir $fileName
    Copy-Item -Path $sourceFile.FullName -Destination $destination -Force
    Write-Host (T "installed $binaryName from $installSource to $destination" "已安装 $binaryName（来源 $installSource）到 $destination")

    if (-not $primaryCliBinary -and ($binaryName -eq 'hopclaw' -or $binaryName -eq 'openclaw')) {
      $primaryCliBinary = $binaryName
      $primaryCliPath = $destination
    }
  }

  $pathAdded = Ensure-InstallDirOnPath -TargetDir $InstallDir
  Write-Step -Message (T 'Finalizing install' '完成安装收尾') -Percent 92
  Write-NextSteps -PrimaryCli $primaryCliBinary -TargetDir $InstallDir -PathAdded $pathAdded

  if ($runOnboardRequested) {
    if (-not $primaryCliPath) {
      Write-Warning 'HOPCLAW_INSTALL_RUN_ONBOARD was set, but no CLI binary was installed'
    } else {
      switch ($OnboardMode) {
        'interactive' {
          Write-Step -Message (T "Launching $primaryCliBinary onboard" "启动 $primaryCliBinary onboard") -Percent 96
          Write-Host ''
          Write-Host (T "launching $primaryCliBinary onboard..." "正在启动 $primaryCliBinary onboard...")
          & $primaryCliPath onboard
        }
        'web-first' {
          Write-Step -Message (T "Launching $primaryCliBinary onboard --web-first" "启动 $primaryCliBinary onboard --web-first") -Percent 96
          Write-Host ''
          Write-Host (T "launching $primaryCliBinary onboard --web-first..." "正在启动 $primaryCliBinary onboard --web-first...")
          & $primaryCliPath onboard --web-first
        }
      }
    }
  }
} finally {
  Write-Progress -Activity 'Installing HopClaw' -Completed
  if (Test-Path -Path $tempDir) {
    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
  }
}
