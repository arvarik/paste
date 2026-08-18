package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/arvarik/paste/internal/config"
	"github.com/arvarik/paste/internal/storage"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	var err error
	switch args[0] {
	case "backup":
		err = runBackup(args[1:], stdout)
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "restore":
		err = runRestore(args[1:])
	case "import":
		err = runImport(args[1:])
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "Unknown command %q.\n", args[0])
		printUsage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s failed: %v\n", args[0], err)
		return 1
	}
	return 0
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: pastectl <backup|verify|restore|import> [options]")
}

func dataDirectory(set *flag.FlagSet) *string {
	defaultDirectory := os.Getenv("DATA_DIR")
	if defaultDirectory == "" {
		defaultDirectory = "./data"
	}
	return set.String("data-dir", defaultDirectory, "stored data directory")
}

func configureStorage(directory string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	storage.DataDir = filepath.Clean(directory)
	storage.SetStorageLimits(storage.StorageLimits{
		MaxTotalBytes: cfg.MaxStorageBytes, MaxItemBytes: cfg.MaxItemBytes,
		MaxItems: cfg.MaxItems, MaxSearchResults: 100,
		MaxSearchIndexBytes: int(cfg.SearchIndexBytes), MaxCachedContentBytes: cfg.ContentCacheBytes,
		MaxBackupBytes: cfg.BackupLimitBytes,
	})
	return nil
}

func runBackup(args []string, stdout io.Writer) error {
	set := flag.NewFlagSet("backup", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	directory := dataDirectory(set)
	output := set.String("output", "", "backup tar path")
	force := set.Bool("force", false, "replace an existing backup")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return errors.New("--output is required")
	}
	if err := configureStorage(*directory); err != nil {
		return err
	}
	outputPath := filepath.Clean(*output)
	if !*force {
		if _, err := os.Lstat(outputPath); err == nil {
			return fs.ErrExist
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".paste-backup-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := storage.ExportBackup(temporary); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return err
	}
	fmt.Fprintln(stdout, outputPath)
	return nil
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("verify", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	directory := dataDirectory(set)
	if err := set.Parse(args); err != nil {
		fmt.Fprintf(stderr, "verify failed: %v\n", err)
		return 1
	}
	if err := configureStorage(*directory); err != nil {
		fmt.Fprintf(stderr, "verify failed: %v\n", err)
		return 1
	}
	report := storage.VerifyIntegrity()
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		fmt.Fprintf(stderr, "verify failed: %v\n", err)
		return 1
	}
	if !report.Healthy {
		return 2
	}
	return 0
}

func runRestore(args []string) error {
	set := flag.NewFlagSet("restore", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	directory := dataDirectory(set)
	input := set.String("input", "", "backup tar path")
	force := set.Bool("force", false, "confirm replacement of stored items")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return errors.New("--input is required")
	}
	if !*force {
		return errors.New("--force is required for restore")
	}
	if err := configureStorage(*directory); err != nil {
		return err
	}
	file, err := openRegularFile(*input)
	if err != nil {
		return err
	}
	defer file.Close()
	return storage.RestoreBackup(file)
}

func runImport(args []string) error {
	set := flag.NewFlagSet("import", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	directory := dataDirectory(set)
	input := set.String("input", "", "backup tar path")
	overwrite := set.Bool("overwrite", false, "replace items with matching IDs")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return errors.New("--input is required")
	}
	if err := configureStorage(*directory); err != nil {
		return err
	}
	file, err := openRegularFile(*input)
	if err != nil {
		return err
	}
	defer file.Close()
	return storage.ImportBackup(file, *overwrite)
}

func openRegularFile(path string) (*os.File, error) {
	info, err := os.Lstat(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	return os.Open(filepath.Clean(path))
}
