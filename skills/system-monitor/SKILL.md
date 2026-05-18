---
name: system-monitor
description: Monitor system resources including CPU, memory, disk, network, and processes
homepage: https://man7.org/linux/man-pages/man1/top.1.html
user-invocable: true
command-dispatch: tool
command-tool: system-monitor.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: infra.system-monitor
    emoji: "\U0001F4CA"
    requires:
      anyBins:
        - top
        - ps
        - df
    always: false
---
# System Monitor

Monitor system resources, process activity, and disk usage.

## Capabilities

- CPU usage and load averages
- Memory and swap usage
- Disk space and I/O statistics
- Running processes sorted by resource usage
- Network connections and bandwidth
- System uptime and basic info
- Process management (find, inspect, signal)

## Usage

Commands vary between macOS and Linux. This skill provides both variants.

### System Overview

```bash
# Uptime and load averages
uptime

# OS and kernel info
uname -a

# macOS: system profiler summary
system_profiler SPHardwareDataType 2>/dev/null || true

# Linux: system info
hostnamectl 2>/dev/null || cat /etc/os-release 2>/dev/null || true
```

### CPU Usage

```bash
# macOS: CPU usage snapshot
top -l 1 -n 0 | head -10

# Linux: CPU usage snapshot
top -bn1 | head -10

# CPU core count
sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo "unknown"

# Load averages
cat /proc/loadavg 2>/dev/null || sysctl -n vm.loadavg 2>/dev/null || uptime
```

### Memory Usage

```bash
# macOS: memory pressure and usage
vm_stat | head -10
memory_pressure 2>/dev/null || true

# Linux: memory usage
free -h

# Detailed memory info (Linux)
cat /proc/meminfo 2>/dev/null | head -20

# macOS: total physical memory
sysctl -n hw.memsize 2>/dev/null | awk '{printf "%.1f GB\n", $1/1073741824}'
```

### Disk Usage

```bash
# Disk space overview
df -h

# Disk space for specific mount
df -h /

# Largest directories in current path
du -sh * 2>/dev/null | sort -rh | head -10

# Inode usage (Linux)
df -i 2>/dev/null

# macOS: disk usage summary
diskutil list 2>/dev/null || true
```

### Process Management

```bash
# Top processes by CPU
ps aux --sort=-%cpu 2>/dev/null | head -15 || ps aux -r | head -15

# Top processes by memory
ps aux --sort=-%mem 2>/dev/null | head -15 || ps aux -m | head -15

# Find a specific process
ps aux | grep -i "[n]ginx"

# Process tree (Linux)
pstree -p 2>/dev/null || ps -ef --forest 2>/dev/null || ps -ef

# Process details by PID
ps -p 12345 -o pid,ppid,user,%cpu,%mem,etime,command

# Open files by process (requires root on some systems)
lsof -p 12345 2>/dev/null | head -20
```

### Network

```bash
# Listening ports
lsof -iTCP -sTCP:LISTEN -n -P 2>/dev/null || ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null

# Active connections
lsof -iTCP -n -P 2>/dev/null | head -20 || ss -tnp 2>/dev/null | head -20

# Network interfaces
ifconfig 2>/dev/null || ip addr 2>/dev/null

# DNS resolution test
dig example.com +short 2>/dev/null || nslookup example.com 2>/dev/null || host example.com 2>/dev/null
```

### I/O Statistics

```bash
# Linux: I/O stats
iostat 2>/dev/null || true

# macOS: I/O stats
iostat -d 2>/dev/null || true

# Linux: I/O by process
iotop -bon1 2>/dev/null | head -15 || true
```

## Output Format

Present monitoring data in a clear dashboard style:

```
System: macOS 14.2 | 10-core Apple M1 Pro | 16 GB RAM
Uptime: 5 days, 3:42 | Load: 2.1, 1.8, 1.5

CPU:    23% used (user: 18%, sys: 5%)
Memory: 11.2 / 16.0 GB (70%) | Swap: 0 B
Disk:   234 / 500 GB (47%) on /

Top Processes (by CPU):
  PID   USER     %CPU  %MEM  COMMAND
  1234  alice    45.2  3.1   node server.js
  5678  alice    12.8  8.4   chrome
  9012  root      5.1  0.2   WindowServer
```

## Error Handling

- Some commands require elevated privileges. If permission is denied, note this and show what data is available without sudo.
- On macOS, `free` is not available; use `vm_stat` and `sysctl` instead.
- On Linux, `vm_stat` is not available; use `free -h` and `/proc/meminfo`.
- If a monitoring tool is missing, suggest installation via the system package manager.

## Security

- Do not run commands with `sudo` unless the user explicitly requests elevated access.
- Process lists may reveal sensitive information (command-line arguments with passwords). Redact when displaying.
- Network connection details may expose internal hostnames and ports. Be cautious in shared environments.
- Avoid killing processes unless the user explicitly requests it and confirms the PID.
