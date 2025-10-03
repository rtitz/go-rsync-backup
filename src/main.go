package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Backup struct {
	config     Config
	timestamp  string
	snapDir    string
	latestLink string
	logFile    *os.File
}

func main() {
	// Parse command line arguments
	configFile := flag.String("config", "config.json", "Configuration file path")
	dryRun := flag.Bool("dry-run", false, "Perform a dry run (no changes)")
	help := flag.Bool("help", false, "Show help")
	flag.Parse()

	if *help {
		fmt.Println("Go Rsync Backup Tool")
		fmt.Println("Usage: backup [options]")
		flag.PrintDefaults()
		os.Exit(0)
	}

	// Check if running as root
	if os.Geteuid() != 0 {
		fmt.Println("This program must be run as root")
		os.Exit(1)
	}

	// Load configuration
	config, err := LoadConfig(*configFile)
	if err != nil {
		log.Printf("Failed to load config: %v", err)
		os.Exit(1)
	}

	// Override with command line flags
	if *dryRun {
		config.DryRun = true
	}

	backup := NewBackup(config)
	if err := backup.Run(); err != nil {
		log.Printf("Backup failed: %v", err)
		os.Exit(1)
	}
}

func NewBackup(config Config) *Backup {
	timestamp := time.Now().Format("MST_2006-01-02_15.04.05")
	return &Backup{
		config:     config,
		timestamp:  timestamp,
		snapDir:    filepath.Join(config.Destination, timestamp+"_INCOMPLETE"),
		latestLink: filepath.Join(config.Destination, "latest"),
	}
}

func (b *Backup) validateConfig() error {
	if b.config.Source == "" {
		return fmt.Errorf("source path cannot be empty")
	}
	if b.config.Destination == "" {
		return fmt.Errorf("destination path cannot be empty")
	}
	if b.config.Keep < 1 {
		return fmt.Errorf("keep must be at least 1")
	}
	if b.config.CleanupAtPercent < 50 || b.config.CleanupAtPercent > 95 {
		return fmt.Errorf("cleanup_at_percent must be between 50-95")
	}
	return nil
}

func (b *Backup) checkDiskSpace() error {
	if b.isSSHPath(b.config.Destination) {
		return nil // Skip disk check for remote destinations
	}

	cmd := exec.Command("df", "-h", b.config.Destination)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check disk space: %v", err)
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return fmt.Errorf("unexpected df output")
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return fmt.Errorf("unexpected df output format")
	}

	usageStr := strings.TrimSuffix(fields[4], "%")
	usage, err := strconv.Atoi(usageStr)
	if err != nil {
		return fmt.Errorf("failed to parse disk usage: %v", err)
	}

	if usage >= b.config.CleanupAtPercent {
		return fmt.Errorf("disk usage %d%% exceeds cleanup threshold %d%%", usage, b.config.CleanupAtPercent)
	}

	b.log("Disk usage: %d%% (threshold: %d%%)", usage, b.config.CleanupAtPercent)
	return nil
}

func (b *Backup) verifyBackup() error {
	if b.config.DryRun {
		return nil // Skip verification for dry runs
	}

	// Check if backup directory exists and has content
	if _, err := os.Stat(b.snapDir); os.IsNotExist(err) {
		return fmt.Errorf("backup directory not created: %s", b.snapDir)
	}

	// Count files in backup
	entries, err := os.ReadDir(b.snapDir)
	if err != nil {
		return fmt.Errorf("failed to read backup directory: %v", err)
	}

	if len(entries) == 0 {
		return fmt.Errorf("backup directory is empty")
	}

	b.log("Backup verification: %d items in backup", len(entries))
	return nil
}

func (b *Backup) Run() error {
	// Validate configuration
	if err := b.validateConfig(); err != nil {
		return fmt.Errorf("config validation failed: %v", err)
	}

	// Setup signal handling
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-c
		b.cleanup(sig, 1)
	}()

	// Validate paths
	if err := b.validatePaths(); err != nil {
		return fmt.Errorf("path validation failed: %v", err)
	}

	// Check disk space
	if err := b.checkDiskSpace(); err != nil {
		return fmt.Errorf("disk space check failed: %v", err)
	}

	// Create lock
	if err := b.createLock(); err != nil {
		return fmt.Errorf("failed to create lock: %v", err)
	}
	defer b.removeLock()

	// Setup logging
	if err := b.setupLogging(); err != nil {
		return fmt.Errorf("failed to setup logging: %v", err)
	}
	defer b.logFile.Close()

	b.log("Starting backup: %s", b.timestamp)

	// Find rsync binary
	if err := b.findRsync(); err != nil {
		return fmt.Errorf("failed to find rsync: %v", err)
	}

	// Get last backup
	lastBackup := b.getLastBackup()
	b.log("Last backup: %s", lastBackup)

	// Run rsync
	if err := b.runRsync(lastBackup); err != nil {
		return fmt.Errorf("rsync failed: %v", err)
	}

	// Verify backup integrity
	if err := b.verifyBackup(); err != nil {
		return fmt.Errorf("backup verification failed: %v", err)
	}

	// Finalize backup (remove _INCOMPLETE suffix)
	if err := b.finalizeBackup(); err != nil {
		return fmt.Errorf("failed to finalize backup: %v", err)
	}

	// Update latest link
	if err := b.updateLatestLink(); err != nil {
		return fmt.Errorf("failed to update latest link: %v", err)
	}

	// Cleanup old backups
	if err := b.cleanupOldBackups(); err != nil {
		b.log("Warning: cleanup failed: %v", err)
	}

	b.log("Backup completed successfully")
	return nil
}

func (b *Backup) validatePaths() error {
	// Create destination directory
	if err := os.MkdirAll(b.config.Destination, 0755); err != nil {
		return fmt.Errorf("failed to create destination: %v", err)
	}

	// Check source exists
	if _, err := os.Stat(b.config.Source); os.IsNotExist(err) {
		return fmt.Errorf("source does not exist: %s", b.config.Source)
	}

	// Check if paths are accessible
	if err := exec.Command("df", b.config.Source).Run(); err != nil {
		return fmt.Errorf("source path %s is not accessible or mounted", b.config.Source)
	}

	if err := exec.Command("df", b.config.Destination).Run(); err != nil {
		return fmt.Errorf("destination path %s is not accessible or mounted", b.config.Destination)
	}

	return nil
}

func (b *Backup) createLock() error {
	if err := os.Mkdir(b.config.LockFile, 0755); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("backup already running (lock: %s). If not, remove the lock directory manually", b.config.LockFile)
		}
		return fmt.Errorf("failed to create lock: %v", err)
	}
	return nil
}

func (b *Backup) removeLock() {
	os.RemoveAll(b.config.LockFile)
}

func (b *Backup) cleanup(sig os.Signal, exitCode int) {
	if b.logFile != nil {
		b.log("Backup interrupted by signal: %v", sig)
	}
	b.removeLock()
	os.Exit(exitCode)
}

func (b *Backup) setupLogging() error {
	// Create log directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(b.config.LogFile), 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	var err error
	b.logFile, err = os.OpenFile(b.config.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}

	// Add separator
	fmt.Fprintf(b.logFile, "\n%s\n", strings.Repeat("=", 80))

	// Cleanup log if needed
	b.cleanupLog()

	return nil
}

func (b *Backup) log(format string, args ...interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)
	logLine := fmt.Sprintf("%s %s\n", timestamp, message)

	fmt.Print(logLine)
	if b.logFile != nil {
		b.logFile.WriteString(logLine)
	}
}

func (b *Backup) cleanupLog() {
	file, err := os.Open(b.config.LogFile)
	if err != nil {
		return
	}
	defer file.Close()

	jobCount := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "Starting backup:") {
			jobCount++
		}
	}

	if jobCount >= 30 {
		// Keep last 500 lines
		cmd := exec.Command("tail", "-n", "500", b.config.LogFile)
		output, err := cmd.Output()
		if err == nil {
			os.WriteFile(b.config.LogFile+".tmp", output, 0644)
			os.Rename(b.config.LogFile+".tmp", b.config.LogFile)
			b.log("Log cleaned up (was %d jobs, kept last 500 lines)", jobCount)
		}
	}
}

func (b *Backup) findRsync() error {
	if b.config.ForceSystemRsync {
		b.config.RsyncBin = "/usr/bin/rsync"
		b.log("Using system rsync (forced by ForceSystemRsync=true)")
		return nil
	}

	paths := []string{
		"/opt/homebrew/bin/rsync", // macOS Homebrew (Apple Silicon)
		"/usr/local/bin/rsync",    // macOS Homebrew (Intel) / Linux
		"/usr/bin/rsync",          // System rsync (macOS/Linux)
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			b.config.RsyncBin = path
			break
		}
	}

	if b.config.RsyncBin == "" {
		return fmt.Errorf("no rsync binary found")
	}

	// Check if it's the old system rsync and warn
	if b.config.RsyncBin == "/usr/bin/rsync" && !b.config.ForceSystemRsync {
		version, err := b.getRsyncVersion()
		if err == nil && b.isOldRsync(version) {
			return fmt.Errorf("homebrew rsync not found. The built-in macOS rsync is too old and lacks proper macOS support. Please install Homebrew rsync with: brew install rsync")
		}
	}

	b.log("Using rsync: %s", b.config.RsyncBin)
	return nil
}

func (b *Backup) getRsyncVersion() (string, error) {
	cmd := exec.Command(b.config.RsyncBin, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	re := regexp.MustCompile(`\d+\.\d+\.\d+`)
	version := re.FindString(string(output))
	return version, nil
}

func (b *Backup) isOldRsync(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return true
	}

	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	patch, _ := strconv.Atoi(parts[2])

	versionNum := major*10000 + minor*100 + patch
	return versionNum < 30200 // Less than 3.2.0
}

func (b *Backup) getLastBackup() string {
	target, err := os.Readlink(b.latestLink)
	if err != nil {
		return "(none)"
	}
	return filepath.Base(target)
}

func (b *Backup) isSSHPath(path string) bool {
	return strings.Contains(path, "@") && strings.Contains(path, ":")
}

func (b *Backup) runRsync(lastBackup string) error {
	b.log("SRC=%s DST=%s", b.config.Source, b.config.Destination)

	args := make([]string, len(RsyncBaseArgs))
	copy(args, RsyncBaseArgs)

	// Add SSH args if source or destination is remote
	if b.isSSHPath(b.config.Source) || b.isSSHPath(b.config.Destination) {
		args = append(args, RsyncSSHArgs...)
		b.log("SSH transfer detected - added compression and SSH options")
	}

	// Add progress flag if enabled
	if b.config.ShowProgress {
		args = append(args, "--progress")
	}

	// Add macOS-specific flags based on rsync version and OS
	version, err := b.getRsyncVersion()
	if err == nil {
		b.log("Detected rsync version: %s", version)
		if runtime.GOOS == "darwin" && !b.isOldRsync(version) {
			args = append(args, RsyncMacOSArgs...)
			b.log("Added macOS-specific flags (modern rsync with full macOS support)")
		} else if runtime.GOOS == "darwin" {
			b.log("Warning: Old rsync version - limited macOS support")
		}
	}

	// Add link-dest if previous backup exists
	if lastBackup != "(none)" {
		lastBackupPath := filepath.Join(b.config.Destination, lastBackup)
		if _, err := os.Stat(lastBackupPath); err == nil {
			args = append(args, "--link-dest="+lastBackupPath)
			b.log("Using link-dest: %s", lastBackupPath)
		}
	} else {
		b.log("No previous backup found for hard linking")
	}

	// Add exclude file if it exists
	if _, err := os.Stat(b.config.ExcludeList); err == nil {
		args = append(args, "--exclude-from="+b.config.ExcludeList)
	} else if b.config.ExcludeList != "" {
		b.log("Warning: exclude list not found at %s — continuing without excludes", b.config.ExcludeList)
	}

	// Add dry-run if configured
	if b.config.DryRun {
		args = append(args, "--dry-run")
		b.log("DRY RUN MODE - no changes will be made")
	}

	// Add source and destination
	args = append(args, b.config.Source+"/", b.snapDir)

	cmdStr := b.config.RsyncBin + " " + strings.Join(args, " ")
	b.log("Running rsync: %s", cmdStr)
	time.Sleep(time.Millisecond * 3000)

	cmd := exec.Command(b.config.RsyncBin, args...)
	
	// Use buffers to capture output while displaying it
	var stdoutBuf, stderrBuf strings.Builder
	
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Copy output to both console and buffer simultaneously
	go io.Copy(io.MultiWriter(os.Stdout, &stdoutBuf), stdoutPipe)
	go io.Copy(io.MultiWriter(os.Stderr, &stderrBuf), stderrPipe)

	if err := cmd.Wait(); err != nil {
		return err
	}

	// Parse transferred data from captured output
	combinedOutput := stdoutBuf.String() + stderrBuf.String()
	gb := b.parseTransferredGB(combinedOutput)
	msg := fmt.Sprintf("Data transferred: %.2f GB", gb)
	fmt.Println(msg)
	b.log("%s", msg)

	return nil
}

func (b *Backup) parseTransferredGB(statsOutput string) float64 {
	// Try multiple patterns for different rsync versions
	patterns := []string{
		`Total transferred file size: ([0-9,]+) bytes`,
		`sent ([0-9,]+) bytes`,
		`total size is ([0-9,]+)`,
	}
	
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(statsOutput)
		if len(matches) > 1 {
			// Remove commas and convert to int64
			bytesStr := strings.ReplaceAll(matches[1], ",", "")
			if bytes, err := strconv.ParseInt(bytesStr, 10, 64); err == nil {
				return float64(bytes) / (1024 * 1024 * 1024) // Convert to GB
			}
		}
	}
	return 0
}

func (b *Backup) finalizeBackup() error {
	if b.config.DryRun {
		return nil // Skip for dry runs
	}
	
	// Rename from _INCOMPLETE to final name
	finalDir := filepath.Join(b.config.Destination, b.timestamp)
	if err := os.Rename(b.snapDir, finalDir); err != nil {
		return fmt.Errorf("failed to rename backup directory: %v", err)
	}
	
	// Update snapDir to final name
	b.snapDir = finalDir
	b.log("Backup finalized: %s", b.timestamp)
	return nil
}

func (b *Backup) updateLatestLink() error {
	// Remove existing link
	os.Remove(b.latestLink)

	// Create new link
	return os.Symlink(b.timestamp, b.latestLink)
}

func (b *Backup) cleanupOldBackups() error {
	if b.config.Keep <= 0 {
		return nil
	}

	entries, err := os.ReadDir(b.config.Destination)
	if err != nil {
		return err
	}

	var backups []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "latest" && !strings.HasSuffix(entry.Name(), "_INCOMPLETE") {
			backups = append(backups, entry.Name())
		}
	}

	if len(backups) <= b.config.Keep {
		return nil
	}

	// Sort backups (oldest first)
	// Simple string sort works for timestamp format
	for i := 0; i < len(backups)-1; i++ {
		for j := i + 1; j < len(backups); j++ {
			if backups[i] > backups[j] {
				backups[i], backups[j] = backups[j], backups[i]
			}
		}
	}

	// Remove oldest backups
	toRemove := len(backups) - b.config.Keep
	for i := 0; i < toRemove; i++ {
		backupPath := filepath.Join(b.config.Destination, backups[i])
		b.log("Removing old backup: %s", backups[i])
		if err := os.RemoveAll(backupPath); err != nil {
			b.log("Warning: failed to remove %s: %v", backupPath, err)
		}
	}

	return nil
}
