# Read-only Windows probe, run through ordinary SSH; no desktop lease needed.
param([string]$OptionsBase64 = "e30=")
$ErrorActionPreference = "Stop"
$options = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($OptionsBase64)) | ConvertFrom-Json
$os = Get-CimInstance Win32_OperatingSystem
$cpu = Get-CimInstance Win32_PerfFormattedData_PerfOS_Processor -Filter "Name='_Total'"
$usage = @{}
Get-CimInstance Win32_PerfFormattedData_PerfProc_Process | ForEach-Object {
    $usage[[int]$_.IDProcess] = [double]$_.PercentProcessorTime
}
$all = @(Get-CimInstance Win32_Process | ForEach-Object {
    if ($_.ProcessId -gt 0) {
        $line = [string]$_.CommandLine
        if (-not $line) { $line = [string]$_.Name }
        if ($line.Length -gt 400) { $line = $line.Substring(0, 400) }
        @{pid=[int]$_.ProcessId; command=$line; cwd=""; rss_bytes=[double]$_.WorkingSetSize; cpu_percent=$usage[[int]$_.ProcessId]}
    }
})
$selected = @($all | Sort-Object rss_bytes -Descending | Select-Object -First 10)
$selected += @($all | Sort-Object cpu_percent -Descending | Select-Object -First 10)
$gpus = @()
$warnings = @()
$smi = Get-Command nvidia-smi.exe -ErrorAction SilentlyContinue
if ($smi) {
    $lines = & $smi.Source --query-gpu=name,utilization.gpu,memory.total,memory.used --format=csv,noheader,nounits
    foreach ($line in $lines) {
        $parts = $line -split ','
        $used = $null
        $util = $null
        if ($parts[3].Trim() -match '^[\d.]+$') { $used = [double]$parts[3] * 1MB }
        if ($parts[1].Trim() -match '^[\d.]+$') { $util = [double]$parts[1] }
        $gpus += @{name=$parts[0].Trim(); utilization_percent=$util; memory_total_bytes=[double]$parts[2]*1MB; memory_used_bytes=$used; memory_note="Dedicated VRAM; WDDM may omit memory usage"}
    }
} else { $warnings += "nvidia-smi unavailable; GPU usage unknown" }
$paths = @{}
foreach ($path in $options.paths) { $paths[$path] = [bool](Test-Path -LiteralPath $path) }
$disks = @(Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" | ForEach-Object {
    @{path=$_.DeviceID; total_bytes=[double]$_.Size; free_bytes=[double]$_.FreeSpace}
})
@{
    sampled_at=([DateTimeOffset]::UtcNow.ToUnixTimeSeconds()); platform=$os.Caption
    cpu_count=[Environment]::ProcessorCount; cpu_percent=[double]$cpu.PercentProcessorTime
    memory=@{total_bytes=[double]$os.TotalVisibleMemorySize*1KB; available_bytes=[double]$os.FreePhysicalMemory*1KB}
    processes=@($selected | Sort-Object pid -Unique); gpus=$gpus; disks=$disks
    containers=@(); paths=$paths; warnings=$warnings
} | ConvertTo-Json -Depth 7 -Compress
