# Go Rsync Backup

A robust, cross-platform backup tool built in Go that wraps rsync with advanced features like incremental backups, SSH support, integrity verification, and comprehensive logging.

## Features

- **Incremental Backups** - Hard-linked snapshots for space efficiency
- **Cross-Platform** - Works on macOS and Linux
- **SSH Support** - Automatic detection and optimization for remote backups
- **Real-Time Progress** - Live rsync output with transfer statistics
- **Integrity Verification** - Automatic backup validation
- **Robust Error Handling** - Comprehensive validation and error reporting
- **Disk Space Management** - Pre-backup space checks and automatic cleanup
- **Signal Handling** - Graceful interruption with CTRL+C
- **Incomplete Backup Protection** - `_INCOMPLETE` suffix prevents corruption

## Installation

```bash
cd src
go build -o backup .
```

## Configuration

The tool uses `config.json` by default if no `-config` parameter is specified. A default configuration file is included in the `src/` directory.

Create or modify `config.json`:

```json
{
  "source": "/Volumes/external-0",
  "destination": "/Volumes/backup-0/backups",
  "keep": 30,
  "cleanup_at_percent": 95,
  "exclude_list": "/Volumes/external-0/.backup-exclude.list",
  "log_file": "/Volumes/backup-0/backups/backup.log",
  "lock_file": "/tmp/backupRunningLock",
  "dry_run": false,
  "force_system_rsync": false,
  "show_progress": true
}
```

### Configuration Options

| Option | Description | Default |
|--------|-------------|----------|
| `source` | Source directory to backup | Required |
| `destination` | Backup destination directory | Required |
| `keep` | Number of backups to retain | 30 |
| `cleanup_at_percent` | Disk usage threshold for cleanup | 95 |
| `exclude_list` | Path to rsync exclude file | Optional |
| `log_file` | Log file path | `/Volumes/backup-0/backups/backup.log` |
| `lock_file` | Lock file to prevent concurrent runs | `/tmp/backupRunningLock` |
| `dry_run` | Test mode without making changes | false |
| `force_system_rsync` | Force use of system rsync | false |
| `show_progress` | Show real-time progress | true |

## Usage

### Basic Usage
```bash
# Uses config.json by default
sudo ./backup

# Or specify a different config file
sudo ./backup -config config.json
```

### Dry Run
```bash
sudo ./backup -config config.json -dry-run
```

### Command Line Options
- `-config` - Configuration file path (default: config.json)
- `-dry-run` - Perform dry run without making changes
- `-help` - Show help message

## SSH Support

SSH transfers are automatically detected and optimized:

```json
{
  "source": "user@server.com:/home/user/data",
  "destination": "/local/backup"
}
```

Or:

```json
{
  "source": "/local/data",
  "destination": "user@backup-server:/backups"
}
```

## Backup Process

1. **Validation** - Config and path validation
2. **Disk Space Check** - Ensures sufficient space
3. **Lock Creation** - Prevents concurrent backups
4. **Rsync Execution** - Creates `TIMESTAMP_INCOMPLETE` directory
5. **Verification** - Validates backup integrity
6. **Finalization** - Removes `_INCOMPLETE` suffix
7. **Latest Link** - Updates symlink to newest backup
8. **Cleanup** - Removes old backups based on `keep` setting

## Rsync Arguments

### Base Arguments
- `-a` - Archive mode (recursive, preserve permissions, times, etc.)
- `-U` - Preserve access times
- `--numeric-ids` - Don't map uid/gid by name
- `-H` - Preserve hard links
- `-A` - Preserve ACLs
- `--partial` - Keep partially transferred files
- `--itemize-changes` - Show file changes
- `--delete` - Delete extraneous files
- `--stats` - Show transfer statistics

**HINT:** "-X" - Extended attributes (can cause excessive disk usage for incementals) can be enabled in "src/variables.go"

### macOS-Specific (Auto-detected)
- `-E` - Preserve executability
- `--fileflags` - Preserve file flags

### SSH-Specific (Auto-detected)
- `-z` - Compress data
- `--compress-level=6` - Compression level
- `-e ssh` - SSH transport with security options

## Logging

Logs include:
- Backup start/completion timestamps
- Rsync command executed
- Transfer statistics (GB transferred)
- Warnings and errors
- Cleanup operations

Example log entry:
```
2025-10-03 13:14:08 Starting backup: CEST_2025-10-03_13.14.08
2025-10-03 13:14:08 Using rsync: /opt/homebrew/bin/rsync
2025-10-03 13:14:08 Running rsync: /opt/homebrew/bin/rsync -a -U ...
2025-10-03 13:30:38 Data transferred: 15.67 GB
2025-10-03 13:30:38 Backup completed successfully
```

## Error Handling

The tool provides comprehensive error handling:
- **Config validation** - Missing or invalid settings
- **Path validation** - Source/destination accessibility
- **Disk space checks** - Insufficient space warnings
- **Lock conflicts** - Concurrent backup prevention
- **Backup verification** - Empty or failed backup detection

## Requirements

- **Go 1.19+** for building
- **rsync 3.2.0+** recommended (Homebrew version on macOS)
- **Root privileges** for system-level backups
- **SSH keys** configured for remote backups

## Platform Support

- **macOS** - Full support with extended attributes
- **Linux** - Full support with standard attributes
- **Cross-platform** - Automatic OS detection and optimization

## License

MIT License - see LICENSE file for details.
