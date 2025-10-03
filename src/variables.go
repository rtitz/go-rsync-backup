package main

const (
	AppName    = "Go-Rsync-Backup"
	AppVersion = "1.0.1"
)

// Default configuration values
var DefaultConfig = Config{
	Source:           "/Volumes/external-0",
	Destination:      "/Volumes/backup-0/backups",
	Keep:             30,
	CleanupAtPercent: 95,
	ExcludeList:      "/Volumes/external-0/.backup-exclude.list",
	LogFile:          "/Volumes/backup-0/backups/backup.log",
	LockFile:         "/tmp/backupRunningLock",
	DryRun:           false,
	ForceSystemRsync: false,
	ShowProgress:     true,
	RsyncBin:         "",
}

// Base rsync arguments with comments
var RsyncBaseArgs = []string{
	"-a",            // Archive mode (recursive, preserve permissions, times, etc.)
	"-U",            // Preserve access times (atimes)
	"--numeric-ids", // Don't map uid/gid values by user/group name
	"-H",            // Preserve hard links
	"-A",            // Preserve ACLs (Access Control Lists)
	//"-X",                // Extended attributes (can cause excessive disk usage for incementals)
	"--partial",         // Keep partially transferred files
	"--itemize-changes", // Output a change-summary for all updates
	"--delete",          // Delete extraneous files from destination
	"--delete-excluded", // Delete excluded files from destination
	"--stats",           // Give some file-transfer stats
}

// macOS-specific rsync arguments (for modern rsync versions)
var RsyncMacOSArgs = []string{
	"-E",          // Preserve executability
	"--fileflags", // Preserve file flags (macOS specific)
}

// SSH-specific rsync arguments
var RsyncSSHArgs = []string{
	"-z",                                                                    // Compress file data during transfer
	"--compress-level=6",                                                    // Compression level (1-9, 6 is good balance)
	"-e", "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", // SSH options
}
