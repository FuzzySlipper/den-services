"""Read-only Linux telemetry. Also sent over SSH stdin; only stdlib required."""
import csv
import glob
import json
import os
import platform
import shutil
import subprocess
import sys
import time
from pathlib import Path


def command(argv, timeout=5):
    try:
        p = subprocess.run(argv, capture_output=True, text=True, timeout=timeout)
        return p.stdout.strip(), p.stderr.strip() if p.returncode else None
    except (OSError, subprocess.TimeoutExpired) as error:
        return "", str(error)


def number(path):
    try:
        return int(Path(path).read_text().strip())
    except (OSError, ValueError):
        return None


def processes():
    result = {}
    for path in Path("/proc").iterdir():
        if not path.name.isdigit():
            continue
        try:
            stat = (path / "stat").read_text().rsplit(")", 1)[1].split()
            result[int(path.name)] = {"ticks": int(stat[11]) + int(stat[12]),
                                      "start": stat[19], "rss": int(stat[21]) * os.sysconf("SC_PAGE_SIZE")}
        except (OSError, ValueError, IndexError):
            pass
    return result


def cpu():
    values = [int(n) for n in Path("/proc/stat").read_text().splitlines()[0].split()[1:9]]
    return sum(values), values[3] + values[4]


def gpu_stats(warnings):
    gpus = []
    smi = shutil.which("nvidia-smi") or ("/usr/lib/wsl/lib/nvidia-smi" if Path("/usr/lib/wsl/lib/nvidia-smi").exists() else None)
    if smi:
        output, error = command([smi, "--query-gpu=name,utilization.gpu,memory.total,memory.used",
                                 "--format=csv,noheader,nounits"])
        if error:
            warnings.append(f"GPU usage unavailable: {error[:160]}")
        if not error:
            for row in csv.reader(output.splitlines()):
                def value(index, scale=1):
                    try:
                        return float(row[index]) * scale
                    except (ValueError, IndexError):
                        return None
                gpus.append({"name": row[0], "utilization_percent": value(1),
                             "memory_total_bytes": value(2, 1048576), "memory_used_bytes": value(3, 1048576),
                             "memory_note": "Dedicated VRAM"})
    for card in glob.glob("/sys/class/drm/card[0-9]*"):
        device = Path(card) / "device"
        if not (device / "mem_info_vram_total").exists():
            continue
        product = device / "product_name"
        gpus.append({"name": product.read_text().strip() if product.exists() else f"AMD {Path(card).name}",
                     "utilization_percent": number(device / "gpu_busy_percent"),
                     "memory_total_bytes": number(device / "mem_info_vram_total"),
                     "memory_used_bytes": number(device / "mem_info_vram_used"),
                     "gtt_used_bytes": number(device / "mem_info_gtt_used"),
                     "memory_note": "Driver VRAM allocation; on Strix Halo RAM is shared, not extra capacity"})
    return gpus


def docker_stats(sockets, warnings):
    containers = []
    if not shutil.which("docker"):
        return containers
    for socket in sockets:
        if not Path(socket).exists():
            continue
        base = ["docker", "-H", "unix://" + socket]
        listing, error = command(base + ["ps", "-a", "--format", "{{json .}}"], timeout=8)
        if error and "permission denied" in error.lower():
            base = ["sudo", "-n"] + base
            listing, error = command(base + ["ps", "-a", "--format", "{{json .}}"], timeout=8)
        if error:
            warnings.append(f"Containers {socket}: {error[:160]}")
            continue
        stats, stats_error = command(base + ["stats", "--no-stream", "--format", "{{json .}}"], timeout=7)
        by_name = {}
        for line in stats.splitlines():
            try:
                item = json.loads(line)
                by_name[item.get("Name")] = item
            except ValueError:
                pass
        if stats_error:
            warnings.append(f"Container usage {socket}: {stats_error[:160]}")
        for line in listing.splitlines():
            try:
                item = json.loads(line)
                usage = by_name.get(item.get("Names"), {})
                containers.append({"name": item.get("Names"), "image": item.get("Image"),
                                   "state": item.get("State"), "status": item.get("Status"),
                                   "cpu": usage.get("CPUPerc"), "memory": usage.get("MemUsage"), "socket": socket})
            except ValueError:
                pass
    return containers


def collect(options):
    warnings = []
    before_cpu, before_proc, before_time = cpu(), processes(), time.monotonic()
    time.sleep(0.35)
    after_cpu, after_proc, elapsed = cpu(), processes(), time.monotonic() - before_time
    delta = after_cpu[0] - before_cpu[0]
    load = round(100 * (1 - (after_cpu[1] - before_cpu[1]) / delta), 1) if delta else None
    hz = os.sysconf("SC_CLK_TCK")
    for pid, item in after_proc.items():
        old = before_proc.get(pid)
        item["cpu_percent"] = round(100 * max(0, item["ticks"] - old["ticks"]) / hz / elapsed, 1) if old and old["start"] == item["start"] else 0
    # Union of heavy CPU and RAM consumers, so a render cannot hide behind a model's RSS.
    selected = set(sorted(after_proc, key=lambda p: after_proc[p]["rss"], reverse=True)[:10])
    selected.update(sorted(after_proc, key=lambda p: after_proc[p]["cpu_percent"], reverse=True)[:10])
    procs = []
    for pid in selected:
        try:
            commandline = Path(f"/proc/{pid}/cmdline").read_bytes().replace(b"\0", b" ").decode(errors="replace").strip()
            if not commandline:
                continue
            try:
                cwd = os.readlink(f"/proc/{pid}/cwd")
            except OSError:
                cwd = ""
            procs.append({"pid": pid, "command": commandline[:400], "cwd": cwd,
                          "rss_bytes": after_proc[pid]["rss"], "cpu_percent": after_proc[pid]["cpu_percent"]})
        except OSError:
            pass
    memory = {}
    for line in Path("/proc/meminfo").read_text().splitlines():
        key, value = line.split(":", 1)
        memory[key] = int(value.strip().split()[0]) * 1024
    disks = []
    for path in options.get("disks", ["/", "/home"]):
        try:
            usage = shutil.disk_usage(path)
            disks.append({"path": path, "total_bytes": usage.total, "free_bytes": usage.free})
        except OSError as error:
            warnings.append(f"Disk {path}: {error}")
    return {"sampled_at": time.time(), "platform": platform.platform(), "cpu_count": os.cpu_count(),
            "cpu_percent": load, "load_average": list(os.getloadavg()),
            "memory": {"total_bytes": memory.get("MemTotal"), "available_bytes": memory.get("MemAvailable"),
                       "swap_total_bytes": memory.get("SwapTotal"), "swap_free_bytes": memory.get("SwapFree")},
            "processes": sorted(procs, key=lambda p: p["cpu_percent"], reverse=True), "disks": disks,
            "gpus": gpu_stats(warnings), "containers": docker_stats(options.get("docker_sockets", []), warnings),
            "paths": {path: Path(path).exists() for path in options.get("paths", [])}, "warnings": warnings}


if __name__ == "__main__":
    print(json.dumps(collect(json.loads(sys.argv[1]) if len(sys.argv) > 1 else {})))
