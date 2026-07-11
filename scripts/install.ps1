# Run this command on Windows:
# irm https://raw.githubusercontent.com/akashkumarpr/wifiler/main/scripts/install.ps1 | iex

$Repo = "akashkumarpr/wifiler"
$Version = "v1.0.0"
$InstallDir = "$HOME\.wifiler"

New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
Write-Host "Downloading wifiler for Windows..." -ForegroundColor Cyan

$Url = "https://github.com/$Repo/releases/download/$Version/wifiler-windows.exe"
Invoke-WebRequest -Uri $Url -OutFile "$InstallDir\wifiler.exe"

$UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($UserPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("PATH", "$UserPath;$InstallDir", "User")
}

# Update the CURRENT terminal session's PATH dynamically so they don't have to restart
if ($env:Path -notlike "*$InstallDir*") {
    $env:Path += ";$InstallDir"
}

Write-Host "[done] wifiler installed successfully! Restart your terminal and run 'wifiler'." -ForegroundColor Green
