[CmdletBinding()]
param(
    [string]$LyraRoot,

    [switch]$Force,

    [string]$ConfigPath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Test-WindowsOrUncPath {
    param([Parameter(Mandatory = $true)][string]$Path)

    if ($Path -match '^[A-Za-z]:[\\/]') {
        return $true
    }

    if ($Path.StartsWith('\\')) {
        return $true
    }

    return $false
}

function Get-LastExitCodeOrZero {
    $lastExitCodeVar = Get-Variable -Name LASTEXITCODE -ErrorAction SilentlyContinue
    if ($null -eq $lastExitCodeVar) {
        $lastExitCodeVar = Get-Variable -Name LASTEXITCODE -Scope Global -ErrorAction SilentlyContinue
    }
    if ($null -eq $lastExitCodeVar) {
        return 0
    }
    return [int]$lastExitCodeVar.Value
}

function Get-ConfigValue {
    param(
        [Parameter(Mandatory = $false)]
        [object]$Config,

        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    if ($null -eq $Config) {
        return $null
    }

    $property = $Config.PSObject.Properties[$Name]
    if ($null -eq $property) {
        return $null
    }

    return $property.Value
}

function Read-LocalConfig {
    param(
        [Parameter(Mandatory = $true)]
        [string]$DefaultPath,

        [Parameter(Mandatory = $false)]
        [string]$SpecifiedPath
    )

    $resolvedPath = $SpecifiedPath
    if ([string]::IsNullOrWhiteSpace($resolvedPath)) {
        if (-not (Test-Path -LiteralPath $DefaultPath -PathType Leaf)) {
            return $null
        }
        $resolvedPath = $DefaultPath
    }
    elseif (-not (Test-Path -LiteralPath $resolvedPath -PathType Leaf)) {
        throw "Config file not found: $resolvedPath"
    }

    try {
        $raw = Get-Content -LiteralPath $resolvedPath -Raw
        if ([string]::IsNullOrWhiteSpace($raw)) {
            throw 'config file is empty'
        }
        return ($raw | ConvertFrom-Json)
    }
    catch {
        throw "failed to parse config file as JSON ($resolvedPath): $($_.Exception.Message)"
    }
}

function Get-FileMap {
    param([Parameter(Mandatory = $true)][string]$Root)

    $result = @{}
    $rootItem = Get-Item -LiteralPath $Root

    $files = Get-ChildItem -LiteralPath $Root -File -Recurse
    foreach ($file in $files) {
        $relative = $file.FullName.Substring($rootItem.FullName.Length).TrimStart([char[]]@('\', '/'))
        $normalizedRelative = $relative -replace '/', '\'
        if ($normalizedRelative.StartsWith('Binaries\', [System.StringComparison]::OrdinalIgnoreCase)) {
            continue
        }
        if ($normalizedRelative.StartsWith('Intermediate\', [System.StringComparison]::OrdinalIgnoreCase)) {
            continue
        }
        $result[$normalizedRelative] = $file.FullName
    }

    return $result
}

function Test-DirectoryEquivalent {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )

    $sourceMap = Get-FileMap -Root $Source
    $destMap = Get-FileMap -Root $Destination

    if ($sourceMap.Count -ne $destMap.Count) {
        return $false
    }

    foreach ($relative in $sourceMap.Keys) {
        if (-not $destMap.ContainsKey($relative)) {
            return $false
        }

        $srcHash = (Get-FileHash -LiteralPath $sourceMap[$relative] -Algorithm SHA256).Hash
        $dstHash = (Get-FileHash -LiteralPath $destMap[$relative] -Algorithm SHA256).Hash
        if ($srcHash -ne $dstHash) {
            return $false
        }
    }

    return $true
}

function Resolve-LyraProfiles {
    param(
        [Parameter(Mandatory = $false)][object]$Config,
        [Parameter(Mandatory = $false)][string]$CliLyraRoot,
        [Parameter(Mandatory = $true)][string]$ConfigPathForError
    )

    $profiles = New-Object System.Collections.Generic.List[object]

    if (-not [string]::IsNullOrWhiteSpace($CliLyraRoot)) {
        $profiles.Add([pscustomobject]@{ Name = 'cli'; LyraRoot = $CliLyraRoot })
        return @($profiles.ToArray())
    }

    $engines = Get-ConfigValue -Config $Config -Name 'engines'
    if ($null -ne $engines) {
        $entries = @($engines.PSObject.Properties | Sort-Object Name)
        if ($entries.Count -eq 0) {
            throw "engines in config is empty ($ConfigPathForError)."
        }

        foreach ($entry in $entries) {
            $engineName = [string]$entry.Name
            $engineCfg = $entry.Value
            $engineLyraRoot = [string](Get-ConfigValue -Config $engineCfg -Name 'lyraRoot')
            if ([string]::IsNullOrWhiteSpace($engineLyraRoot)) {
                $engineLyraRoot = [string](Get-ConfigValue -Config $Config -Name 'lyraRoot')
            }
            if ([string]::IsNullOrWhiteSpace($engineLyraRoot)) {
                throw "engines.$engineName.lyraRoot is required ($ConfigPathForError)."
            }
            $profiles.Add([pscustomobject]@{ Name = $engineName; LyraRoot = $engineLyraRoot })
        }

        return @($profiles.ToArray())
    }

    $flatLyraRoot = [string](Get-ConfigValue -Config $Config -Name 'lyraRoot')
    if ([string]::IsNullOrWhiteSpace($flatLyraRoot)) {
        throw "LyraRoot is required (pass -LyraRoot or set lyraRoot/engines in $ConfigPathForError)."
    }

    $profiles.Add([pscustomobject]@{ Name = 'default'; LyraRoot = $flatLyraRoot })
    return @($profiles.ToArray())
}

function Ensure-BPXPluginEnabledInUProject {
    param(
        [Parameter(Mandatory = $true)][string]$LyraRoot,
        [Parameter(Mandatory = $true)][string]$ProfileName
    )

    $uprojectPath = Join-Path $LyraRoot 'Lyra.uproject'
    if (-not (Test-Path -LiteralPath $uprojectPath -PathType Leaf)) {
        throw "Lyra project file not found: $uprojectPath"
    }

    try {
        $uprojectRaw = Get-Content -LiteralPath $uprojectPath -Raw
        if ([string]::IsNullOrWhiteSpace($uprojectRaw)) {
            throw 'project file is empty'
        }
        $uprojectJson = $uprojectRaw | ConvertFrom-Json
    }
    catch {
        throw "failed to parse Lyra project file as JSON ($uprojectPath): $($_.Exception.Message)"
    }

    $pluginsProperty = $uprojectJson.PSObject.Properties['Plugins']
    if ($null -eq $pluginsProperty) {
        $uprojectJson | Add-Member -NotePropertyName Plugins -NotePropertyValue @()
    }

    $plugins = @($uprojectJson.Plugins)
    $entry = $plugins | Where-Object { ([string]$_.Name) -eq 'BPXFixtureGenerator' } | Select-Object -First 1
    $updated = $false

    if ($null -eq $entry) {
        $newPlugins = New-Object System.Collections.Generic.List[object]
        foreach ($plugin in $plugins) {
            $newPlugins.Add($plugin) | Out-Null
        }
        $newPlugins.Add([pscustomobject]@{
            Name    = 'BPXFixtureGenerator'
            Enabled = $true
        }) | Out-Null
        $uprojectJson.Plugins = @($newPlugins.ToArray())
        $updated = $true
    }
    else {
        $enabledProperty = $entry.PSObject.Properties['Enabled']
        if (($null -eq $enabledProperty) -or (-not [bool]$enabledProperty.Value)) {
            if ($null -eq $enabledProperty) {
                $entry | Add-Member -NotePropertyName Enabled -NotePropertyValue $true
            }
            else {
                $entry.Enabled = $true
            }
            $updated = $true
        }
    }

    if ($updated) {
        $updatedJson = $uprojectJson | ConvertTo-Json -Depth 100
        Set-Content -LiteralPath $uprojectPath -Value $updatedJson -Encoding UTF8
        Write-Host "Enabled BPXFixtureGenerator in Lyra.uproject (profile=$ProfileName)."
    }
    else {
        Write-Host "Lyra.uproject already enables BPXFixtureGenerator (profile=$ProfileName)."
    }
}

function Sync-PluginToLyraRoot {
    param(
        [Parameter(Mandatory = $true)][string]$ProfileName,
        [Parameter(Mandatory = $true)][string]$LyraRoot,
        [Parameter(Mandatory = $true)][string]$SourcePluginDir,
        [Parameter(Mandatory = $true)][bool]$Force
    )

    if (-not (Test-WindowsOrUncPath -Path $LyraRoot)) {
        throw "LyraRoot must be a Windows drive path or UNC path. Input: $LyraRoot"
    }

    if (-not (Test-Path -LiteralPath $LyraRoot -PathType Container)) {
        throw "LyraRoot not found: $LyraRoot"
    }

    $lyraPluginsDir = Join-Path $LyraRoot 'Plugins'
    if (-not (Test-Path -LiteralPath $lyraPluginsDir -PathType Container)) {
        throw "Lyra plugins directory not found: $lyraPluginsDir"
    }

    $destinationPluginDir = Join-Path $lyraPluginsDir 'BPXFixtureGenerator'

    Write-Host ""
    Write-Host "=== Sync BPX plugin profile: $ProfileName ==="
    Write-Host "LyraRoot: $LyraRoot"

    if (Test-Path -LiteralPath $destinationPluginDir -PathType Container) {
        if (-not $Force) {
            $same = Test-DirectoryEquivalent -Source $SourcePluginDir -Destination $destinationPluginDir
            if (-not $same) {
                throw "Destination already exists and differs from source. Re-run with -Force to overwrite: $destinationPluginDir"
            }

            Write-Host "BPX plugin already synced: $destinationPluginDir"
        }
        else {
            Get-ChildItem -LiteralPath $destinationPluginDir -Force | ForEach-Object {
                if ($_.PSIsContainer -and ($_.Name -in @('Binaries', 'Intermediate'))) {
                    return
                }
                Remove-Item -LiteralPath $_.FullName -Recurse -Force
            }
            Copy-Item -Path (Join-Path $SourcePluginDir '*') -Destination $destinationPluginDir -Recurse -Force
            Write-Host "BPX plugin synced (force overwrite): $destinationPluginDir"
        }
    }
    else {
        Copy-Item -LiteralPath $SourcePluginDir -Destination $destinationPluginDir -Recurse
        Write-Host "BPX plugin synced: $destinationPluginDir"
    }

    $upluginPath = Join-Path $destinationPluginDir 'BPXFixtureGenerator.uplugin'
    if (-not (Test-Path -LiteralPath $upluginPath -PathType Leaf)) {
        throw "Sync validation failed: missing .uplugin file at $upluginPath"
    }

    $buildFiles = @(Get-ChildItem -LiteralPath $destinationPluginDir -File -Recurse -Filter '*.Build.cs')
    if ($buildFiles.Count -eq 0) {
        throw "Sync validation failed: no .Build.cs file found under $destinationPluginDir"
    }

    Ensure-BPXPluginEnabledInUProject -LyraRoot $LyraRoot -ProfileName $ProfileName

    Write-Host "Sync validation passed: plugin descriptor and build files are present (profile=$ProfileName)."
}

function Sync-PluginProfilesParallel {
    param(
        [Parameter(Mandatory = $true)][object[]]$Profiles,
        [Parameter(Mandatory = $true)][string]$ScriptPath,
        [Parameter(Mandatory = $true)][bool]$Force
    )

    $jobs = New-Object System.Collections.Generic.List[System.Management.Automation.Job]

    foreach ($profile in $Profiles) {
        $profileName = [string]$profile.Name
        Write-Host "Starting plugin sync job: $profileName"

        $payload = [pscustomobject]@{
            Name      = $profileName
            ScriptPath = $ScriptPath
            LyraRoot  = [string]$profile.LyraRoot
            Force     = $Force
        }

        $job = Start-Job -Name ("bpx-sync-" + $profileName) -ScriptBlock {
            param([Parameter(Mandatory = $true)][object]$Payload)

            $psArgs = @(
                '-NoProfile',
                '-ExecutionPolicy', 'Bypass',
                '-File', [string]$Payload.ScriptPath,
                '-LyraRoot', [string]$Payload.LyraRoot
            )
            if ([bool]$Payload.Force) {
                $psArgs += '-Force'
            }

            & powershell.exe @psArgs
            $exitCode = 0
            $lastExitCodeVar = Get-Variable -Name LASTEXITCODE -ErrorAction SilentlyContinue
            if ($null -ne $lastExitCodeVar) {
                $exitCode = [int]$lastExitCodeVar.Value
            }
            if ($exitCode -ne 0) {
                throw "profile $([string]$Payload.Name) failed with exit code $exitCode"
            }

            Write-Output "profile $([string]$Payload.Name) completed."
        } -ArgumentList $payload

        $jobs.Add($job) | Out-Null
    }

    if ($jobs.Count -eq 0) {
        return
    }

    Wait-Job -Job ($jobs.ToArray()) | Out-Null

    $failedJobs = New-Object System.Collections.Generic.List[string]
    foreach ($job in $jobs) {
        Write-Host ""
        Write-Host "=== Profile job log: $($job.Name) ==="
        Receive-Job -Job $job -ErrorAction Continue

        if ($job.State -ne 'Completed') {
            $failedJobs.Add($job.Name) | Out-Null
        }
    }

    Remove-Job -Job ($jobs.ToArray()) -Force

    if ($failedJobs.Count -gt 0) {
        throw "Plugin sync failed for profile job(s): $($failedJobs -join ', ')"
    }
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = (Resolve-Path (Join-Path $scriptDir '..')).Path
$sourcePluginDir = Join-Path $repoRoot 'testdata/BPXFixtureGenerator'
if (-not (Test-Path -LiteralPath $sourcePluginDir -PathType Container)) {
    throw "Plugin source directory not found: $sourcePluginDir"
}

$defaultConfigPath = Join-Path $scriptDir 'local-fixtures.config.json'
$config = Read-LocalConfig -DefaultPath $defaultConfigPath -SpecifiedPath $ConfigPath
$effectiveConfigPath = $ConfigPath
if ([string]::IsNullOrWhiteSpace($effectiveConfigPath)) {
    $effectiveConfigPath = $defaultConfigPath
}

$profiles = @(Resolve-LyraProfiles -Config $config -CliLyraRoot $LyraRoot -ConfigPathForError $effectiveConfigPath)
if ($profiles.Count -eq 0) {
    throw 'No Lyra profile resolved.'
}

$seenLyraRoots = @{}
$uniqueProfiles = New-Object System.Collections.Generic.List[object]
foreach ($profile in $profiles) {
    $root = [string]$profile.LyraRoot
    if ($seenLyraRoots.ContainsKey($root)) {
        continue
    }
    $seenLyraRoots[$root] = $true
    $uniqueProfiles.Add($profile) | Out-Null
}

if ($uniqueProfiles.Count -eq 1) {
    $single = $uniqueProfiles[0]
    Sync-PluginToLyraRoot -ProfileName ([string]$single.Name) -LyraRoot ([string]$single.LyraRoot) -SourcePluginDir $sourcePluginDir -Force ([bool]$Force)
}
else {
    Write-Host "Launching plugin sync for $($uniqueProfiles.Count) profiles in parallel..."
    Sync-PluginProfilesParallel -Profiles ($uniqueProfiles.ToArray()) -ScriptPath $MyInvocation.MyCommand.Path -Force ([bool]$Force)
}

Write-Host ""
Write-Host "Completed plugin sync for $($uniqueProfiles.Count) profile(s)."
