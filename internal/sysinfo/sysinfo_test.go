package sysinfo_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"vpsbot/internal/sysinfo"
)

func TestSysInfo_ListFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "superbot_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	sys := sysinfo.New(tmpDir)
	out, err := sys.ListFiles(tmpDir)
	if err != nil {
		t.Fatalf("ListFiles returned unexpected error: %v", err)
	}

	if len(out) == 0 {
		t.Error("expected non-empty output from ListFiles")
	}
}

func TestSysInfo_ValidateFileForSending(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "superbot_send_*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	_, _ = tmpFile.WriteString("sample content")
	_ = tmpFile.Close()

	sys := sysinfo.New("/")
	path, err := sys.ValidateFileForSending(tmpFile.Name())
	if err != nil {
		t.Fatalf("expected valid file, got error: %v", err)
	}

	if path != filepath.Clean(tmpFile.Name()) {
		t.Errorf("expected clean path %s, got %s", tmpFile.Name(), path)
	}

	// Test non-existing file
	_, err = sys.ValidateFileForSending("/non_existing_file_12345.xyz")
	if err == nil {
		t.Error("expected error for non-existing file, got nil")
	}
}

func TestSysInfo_GetStats(t *testing.T) {
	sys := sysinfo.New("/")
	stats, err := sys.GetStats()
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if len(stats) == 0 {
		t.Error("expected non-empty stats output")
	}
	t.Logf("\n%s", stats)
	if !strings.Contains(stats, "CPU Load:") {
		t.Error("expected stats to contain 'CPU Load:'")
	}
	if !strings.Contains(stats, "▰") && !strings.Contains(stats, "▱") {
		t.Error("expected stats to contain progress bar characters")
	}
}


func TestSysInfo_GetBackupInfo(t *testing.T) {
	sys := sysinfo.New("/")
	info, err := sys.GetBackupInfo()
	if err != nil {
		t.Fatalf("GetBackupInfo failed: %v", err)
	}
	if len(info) == 0 {
		t.Error("expected non-empty backup info")
	}
}
