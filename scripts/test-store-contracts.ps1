<#
.SYNOPSIS
    Runs store contract tests against SQLite and ephemeral MariaDB 11.4.
.DESCRIPTION
    Harness script to run store contract tests in isolated environments.
    Usage:
        pwsh -NoProfile -File scripts/test-store-contracts.ps1 -Backend sqlite
        pwsh -NoProfile -File scripts/test-store-contracts.ps1 -Backend mariadb
        pwsh -NoProfile -File scripts/test-store-contracts.ps1 -Backend all
#>

[CmdletBinding()]
param (
    [ValidateSet("sqlite", "mariadb", "all")]
    [string]$Backend = "sqlite"
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = Split-Path -Parent $ScriptDir
$ComposeFile = Join-Path $RepoRoot "test/mariadb/compose.yml"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    $candidateToolchain = Join-Path (Split-Path -Parent $RepoRoot) "toolchains\go1.26.4\bin"
    if (Test-Path $candidateToolchain) {
        $env:PATH = "$candidateToolchain;$env:PATH"
    }
}

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    $dockerBin = "C:\Program Files\Docker\Docker\resources\bin"
    if (Test-Path $dockerBin) {
        $env:PATH = "$dockerBin;$env:PATH"
    }
}


function Generate-RandomString([int]$length = 24) {
    $chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    $bytes = New-Object byte[] $length
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    $rng.GetBytes($bytes)
    $result = New-Object char[] $length
    for ($i = 0; $i -lt $length; $i++) {
        $result[$i] = $chars[$bytes[$i] % $chars.Length]
    }
    return -join $result
}

function Detect-DockerCompose() {
    try {
        & docker compose version *>$null
        if ($LASTEXITCODE -eq 0) {
            return @("docker", "compose")
        }
    } catch {}

    try {
        & docker-compose version *>$null
        if ($LASTEXITCODE -eq 0) {
            return @("docker-compose")
        }
    } catch {}

    throw "Neither 'docker compose' nor 'docker-compose' found in PATH."
}

function Run-Sqlite() {
    Write-Host "==> Running store contract tests against SQLite..." -ForegroundColor Cyan
    Push-Location $RepoRoot
    try {
        $env:WACALLS_TEST_BACKEND = "sqlite"
        go test ./internal/testdb/... -run '^TestStoreHarnessContract$' -count=1 -timeout=5m
        if ($LASTEXITCODE -ne 0) {
            throw "SQLite store contract test failed with exit code $LASTEXITCODE"
        }
        go test ./cmd/server/... -run '^TestSupportStore' -count=1 -timeout=5m
        if ($LASTEXITCODE -ne 0) {
            throw "SQLite support store contract test failed with exit code $LASTEXITCODE"
        }
    } finally {
        Remove-Item Env:\WACALLS_TEST_BACKEND -ErrorAction SilentlyContinue
        Pop-Location
    }
}

function Run-MariaDB() {
    Write-Host "==> Preparing ephemeral MariaDB environment..." -ForegroundColor Cyan

    & docker --version *>$null
    if ($LASTEXITCODE -ne 0) {
        throw "Docker CLI not found or not working."
    }

    & docker info *>$null
    if ($LASTEXITCODE -ne 0) {
        throw "Docker daemon is not accessible or not running."
    }

    $composeCmd = Detect-DockerCompose

    if (-not (Test-Path $ComposeFile)) {
        throw "Compose file not found at: $ComposeFile"
    }

    $randomSuffix = [System.Guid]::NewGuid().ToString("N").Substring(0, 8)
    $projectName = "wacalls-store-contract-$randomSuffix"
    $dbName = "wacalls_store_test_$randomSuffix"
    $dbUser = "wacalls_test_$randomSuffix"
    $rootPass = Generate-RandomString 24
    $userPass = Generate-RandomString 24

    $cleanupDone = $false

    try {
        Write-Host "==> Starting ephemeral MariaDB container ($projectName)..." -ForegroundColor Cyan
        $env:MARIADB_ROOT_PASSWORD = $rootPass
        $env:MARIADB_DATABASE = $dbName
        $env:MARIADB_USER = $dbUser
        $env:MARIADB_PASSWORD = $userPass

        if ($composeCmd.Length -eq 2) {
            & docker compose -p $projectName -f $ComposeFile up -d
        } else {
            & docker-compose -p $projectName -f $ComposeFile up -d
        }
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to start MariaDB container."
        }

        Write-Host "==> Waiting for MariaDB readiness (max 90s)..." -ForegroundColor Cyan
        $ready = $false
        $deadline = (Get-Date).AddSeconds(90)

        while ((Get-Date) -lt $deadline) {
            $portMapping = $null
            if ($composeCmd.Length -eq 2) {
                $portMapping = (& docker compose -p $projectName -f $ComposeFile port mariadb 3306 2>$null)
            } else {
                $portMapping = (& docker-compose -p $projectName -f $ComposeFile port mariadb 3306 2>$null)
            }

            if ($portMapping) {
                # Test connectivity using ephemeral user inside container
                if ($composeCmd.Length -eq 2) {
                    & docker compose -p $projectName -f $ComposeFile exec -T mariadb mariadb -u $dbUser "-p$userPass" $dbName -e "SELECT 1;" *>$null
                } else {
                    & docker-compose -p $projectName -f $ComposeFile exec -T mariadb mariadb -u $dbUser "-p$userPass" $dbName -e "SELECT 1;" *>$null
                }
                if ($LASTEXITCODE -eq 0) {
                    $ready = $true
                    break
                }
            }
            Start-Sleep -Seconds 2
        }

        if (-not $ready) {
            throw "MariaDB failed to become ready within 90 seconds."
        }

        $portMapping = $null
        if ($composeCmd.Length -eq 2) {
            $portMapping = (& docker compose -p $projectName -f $ComposeFile port mariadb 3306)
        } else {
            $portMapping = (& docker-compose -p $projectName -f $ComposeFile port mariadb 3306)
        }
        $hostPort = ($portMapping -split ":")[-1].Trim()

        $mariadbDSN = "$($dbUser):$($userPass)@tcp(127.0.0.1:$($hostPort))/$($dbName)?parseTime=true&timeout=10s"

        Write-Host "==> Running store contract tests against MariaDB 11.4 (port $hostPort)..." -ForegroundColor Cyan
        Push-Location $RepoRoot
        try {
            $env:WACALLS_TEST_BACKEND = "mariadb"
            $env:WACALLS_TEST_MARIADB_DSN = $mariadbDSN
            go test ./internal/testdb/... -run '^TestStoreHarnessContract$' -count=1 -timeout=5m
            if ($LASTEXITCODE -ne 0) {
                throw "MariaDB store contract test failed with exit code $LASTEXITCODE"
            }
            go test ./cmd/server/... -run '^TestSupportStore' -count=1 -timeout=5m
            if ($LASTEXITCODE -ne 0) {
                throw "MariaDB support store contract test failed with exit code $LASTEXITCODE"
            }
        } finally {
            Remove-Item Env:\WACALLS_TEST_BACKEND -ErrorAction SilentlyContinue
            Remove-Item Env:\WACALLS_TEST_MARIADB_DSN -ErrorAction SilentlyContinue
            Pop-Location
        }
    } finally {
        if (-not $cleanupDone) {
            $cleanupDone = $true
            if ($projectName -and $projectName -like "wacalls-store-contract-*") {
                Write-Host "==> Cleaning up MariaDB test environment for project '$projectName'..." -ForegroundColor Yellow
                if ($composeCmd.Length -eq 2) {
                    & docker compose -p $projectName -f $ComposeFile down -v --remove-orphans *>$null
                } else {
                    & docker-compose -p $projectName -f $ComposeFile down -v --remove-orphans *>$null
                }
            }
            Remove-Item Env:\MARIADB_ROOT_PASSWORD -ErrorAction SilentlyContinue
            Remove-Item Env:\MARIADB_DATABASE -ErrorAction SilentlyContinue
            Remove-Item Env:\MARIADB_USER -ErrorAction SilentlyContinue
            Remove-Item Env:\MARIADB_PASSWORD -ErrorAction SilentlyContinue
        }
    }
}

switch ($Backend) {
    "sqlite" { Run-Sqlite }
    "mariadb" { Run-MariaDB }
    "all" {
        Run-Sqlite
        Run-MariaDB
    }
}

Write-Host "==> All requested store contract tests completed successfully." -ForegroundColor Green
