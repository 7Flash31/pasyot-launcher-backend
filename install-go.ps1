# Downloads the official Go toolchain into .toolchain\go next to this file.
# Nothing outside that folder is touched: no PATH changes, no installers,
# no administrator rights. Delete the folder to undo.
$ErrorActionPreference = 'Stop'
Set-Location -Path $PSScriptRoot

$version = ((Invoke-WebRequest -UseBasicParsing 'https://go.dev/VERSION?m=text').Content).Split([char]10)[0].Trim()
if (-not $version) { throw 'cannot reach go.dev' }

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$file = $version + '.windows-' + $arch + '.zip'

Write-Host ('Go is not installed, downloading ' + $version + ' for windows/' + $arch + ' (about 80 MB)...')
New-Item -ItemType Directory -Force -Path '.toolchain' | Out-Null
$zip = Join-Path '.toolchain' $file
Invoke-WebRequest -UseBasicParsing ('https://go.dev/dl/' + $file) -OutFile $zip

$want = ((Invoke-RestMethod 'https://go.dev/dl/?mode=json').files | Where-Object { $_.filename -eq $file }).sha256
$got = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
if ($want -and $want -ne $got) {
    Remove-Item $zip -Force
    throw 'checksum mismatch, download aborted'
}
Write-Host 'checksum ok'

if (Test-Path '.toolchain\go') { Remove-Item -Recurse -Force '.toolchain\go' }
Expand-Archive -Path $zip -DestinationPath '.toolchain' -Force
Remove-Item $zip -Force
Write-Host 'Go installed into .toolchain\go'
