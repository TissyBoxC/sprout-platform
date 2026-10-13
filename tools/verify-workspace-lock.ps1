[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$workspaceRoot = Split-Path -Parent $PSScriptRoot
$lockFilePath = Join-Path $workspaceRoot 'workspace.lock.yaml'

if (-not (Test-Path -LiteralPath $lockFilePath -PathType Leaf)) {
    throw "Workspace lock file not found: $lockFilePath"
}

$repositoryNames = @('firmware', 'sub2api_fork')
$repositories = @{}
$currentRepository = $null
$insideRepositories = $false

foreach ($line in Get-Content -LiteralPath $lockFilePath) {
    if ($line -match '^repositories:\s*$') {
        $insideRepositories = $true
        continue
    }
    if ($insideRepositories -and $line -match '^\S') {
        break
    }
    if (-not $insideRepositories -or $line -match '^\s*#') {
        continue
    }

    if ($line -match '^  ([a-z0-9_]+):\s*$') {
        if ($null -ne $currentRepository) {
            $repositories[$currentRepository.Name] = $currentRepository
        }
        $currentRepository = @{ Name = $Matches[1] }
        continue
    }
    if ($null -eq $currentRepository) {
        continue
    }
    if ($line -match '^\s{4}(path|url|branch|revision):\s*(.+?)\s*$') {
        $currentRepository[$Matches[1]] = $Matches[2].Trim('"', "'")
    }
}

if ($null -ne $currentRepository) {
    $repositories[$currentRepository.Name] = $currentRepository
}

foreach ($name in $repositoryNames) {
    if (-not $repositories.ContainsKey($name)) {
        throw "Workspace lock entry is missing: $name"
    }

    $repository = $repositories[$name]
    foreach ($field in @('path', 'url', 'branch', 'revision')) {
        if (-not $repository.ContainsKey($field) -or
            [string]::IsNullOrWhiteSpace($repository[$field])) {
            throw "Workspace lock entry is incomplete: $name.$field"
        }
    }

    $checkout = Join-Path $workspaceRoot $repository.Path
    if (-not (Test-Path -LiteralPath $checkout -PathType Container)) {
        throw "Workspace checkout is missing: $checkout"
    }

    $remoteRevision = (
        git -C $checkout rev-parse --verify "origin/$($repository.Branch)"
    ).Trim()
    if ($LASTEXITCODE -ne 0 -or $remoteRevision -eq '') {
        throw "Unable to resolve origin/$($repository.Branch) for $name."
    }

    git -C $checkout merge-base --is-ancestor $repository.Revision $remoteRevision
    if ($LASTEXITCODE -ne 0) {
        throw (
            "Locked revision for $name is not contained in " +
            "origin/$($repository.Branch): $($repository.Revision)"
        )
    }

    Write-Host "verified $name at $($repository.Revision)"
}
