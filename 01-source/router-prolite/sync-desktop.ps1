param(
    [switch]$NoBuild,
    [switch]$NoSource
)

$ErrorActionPreference = 'Stop'
$ProjectRoot = $PSScriptRoot
$DesktopRoot = 'C:\Users\13223\Desktop\RouterProLite'
$SrcDir = Join-Path $DesktopRoot '01-source'
$BinDir = Join-Path $DesktopRoot '02-bin'
$DocsDir = Join-Path $DesktopRoot '03-docs'
$SumDir = Join-Path $DesktopRoot '04-checksums'

$PortableGo = 'C:\Users\13223\Desktop\yuanqijian\7621\work\toolchain\go\bin\go.exe'
$GoExe = if (Test-Path $PortableGo) { $PortableGo } else { 'go' }

New-Item -ItemType Directory -Force -Path $SrcDir, $BinDir, $DocsDir, $SumDir | Out-Null

if (-not $NoBuild) {
    Write-Host '[1/4] Building ProLite.exe ...'
    $outDir = Join-Path $ProjectRoot 'build\bin'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    Push-Location $ProjectRoot
    try {
        & $GoExe build -mod=mod -tags desktop,production -ldflags '-H windowsgui -s -w' -o (Join-Path $outDir 'ProLite.exe') .
        if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
    }
    finally {
        Pop-Location
    }
}

$exe = Join-Path $ProjectRoot 'build\bin\ProLite.exe'
if (-not (Test-Path $exe)) {
    throw "build output not found: $exe"
}
Copy-Item $exe (Join-Path $BinDir 'ProLite.exe') -Force
Copy-Item (Join-Path $ProjectRoot 'docs\BIN-README.md') (Join-Path $BinDir 'README.md') -Force

if (-not $NoSource) {
    Write-Host '[2/4] Syncing source (merge, no deletes) ...'
    $dst = Join-Path $SrcDir 'router-prolite'
    New-Item -ItemType Directory -Force -Path $dst | Out-Null
    robocopy $ProjectRoot $dst /E /XD .git node_modules build dist .trae .idea .vscode /XF *.exe /R:1 /W:1 /NFL /NDL /NJH /NJS /NP | Out-Null
    if ($LASTEXITCODE -ge 8) { throw "robocopy failed with exit code $LASTEXITCODE" }
}

Write-Host '[3/4] Syncing docs ...'
Copy-Item (Join-Path $ProjectRoot 'docs\*.md') $DocsDir -Force

Write-Host '[4/4] Writing SHA256 manifest ...'
$files = @()
foreach ($root in @($SrcDir, $BinDir, $DocsDir)) {
    if (Test-Path $root) {
        $files += Get-ChildItem $root -Recurse -File
    }
}
$lines = foreach ($f in $files) {
    $h = (Get-FileHash $f.FullName -Algorithm SHA256).Hash.ToLower()
    $rel = $f.FullName.Substring($DesktopRoot.Length + 1)
    "$h  $rel"
}
$lines | Set-Content (Join-Path $SumDir 'SHA256SUMS.txt') -Encoding UTF8

$exeInfo = Get-Item (Join-Path $BinDir 'ProLite.exe')
$goVersion = (& $GoExe version) 2>$null
@(
    "build_time: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')",
    "go: $goVersion",
    "exe: 02-bin/ProLite.exe",
    "size: $($exeInfo.Length) bytes",
    "sha256: $((Get-FileHash $exeInfo.FullName -Algorithm SHA256).Hash.ToLower())",
    "source_files: $((Get-ChildItem (Join-Path $SrcDir 'router-prolite') -Recurse -File).Count)"
) | Set-Content (Join-Path $SumDir 'BUILD-INFO.txt') -Encoding UTF8

Write-Host ''
Write-Host 'Sync complete:'
Write-Host "  source $SrcDir\router-prolite"
Write-Host "  binary $BinDir\ProLite.exe"
Write-Host "  docs   $DocsDir"
Write-Host "  hashes $SumDir\SHA256SUMS.txt"
