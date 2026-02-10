package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ========================================
// Test newConverterCommand - Command Structure
// ========================================

func TestNewConverterCommand_Metadata_Use(t *testing.T) {
	cmd := newConverterCommand()
	if cmd.Use != "convert" {
		t.Errorf("Use = %q, want %q", cmd.Use, "convert")
	}
}

func TestNewConverterCommand_Metadata_Short(t *testing.T) {
	cmd := newConverterCommand()
	if cmd.Short == "" {
		t.Error("Short description should not be empty")
	}
}

func TestNewConverterCommand_Metadata_Long(t *testing.T) {
	cmd := newConverterCommand()
	if cmd.Long == "" {
		t.Error("Long description should not be empty")
	}
}

func TestNewConverterCommand_Flags_Input(t *testing.T) {
	cmd := newConverterCommand()
	flag := cmd.Flags().Lookup("input")
	if flag == nil {
		t.Fatal("--input flag not found")
	}
	if flag.Shorthand != "i" {
		t.Errorf("--input shorthand = %q, want %q", flag.Shorthand, "i")
	}
}

func TestNewConverterCommand_Flags_Output(t *testing.T) {
	cmd := newConverterCommand()
	flag := cmd.Flags().Lookup("output")
	if flag == nil {
		t.Fatal("--output flag not found")
	}
	if flag.Shorthand != "o" {
		t.Errorf("--output shorthand = %q, want %q", flag.Shorthand, "o")
	}
}

func TestNewConverterCommand_Flags_Format(t *testing.T) {
	cmd := newConverterCommand()
	flag := cmd.Flags().Lookup("format")
	if flag == nil {
		t.Fatal("--format flag not found")
	}
	if flag.Shorthand != "f" {
		t.Errorf("--format shorthand = %q, want %q", flag.Shorthand, "f")
	}
	if flag.DefValue != "auto" {
		t.Errorf("--format default = %q, want %q", flag.DefValue, "auto")
	}
}

func TestNewConverterCommand_Flags_Verbose(t *testing.T) {
	cmd := newConverterCommand()
	flag := cmd.Flags().Lookup("verbose")
	if flag == nil {
		t.Fatal("--verbose flag not found")
	}
	if flag.Shorthand != "v" {
		t.Errorf("--verbose shorthand = %q, want %q", flag.Shorthand, "v")
	}
}

func TestNewConverterCommand_Flags_Overwrite(t *testing.T) {
	cmd := newConverterCommand()
	flag := cmd.Flags().Lookup("overwrite")
	if flag == nil {
		t.Fatal("--overwrite flag not found")
	}
}

func TestNewConverterCommand_Flags_ExcludeBones(t *testing.T) {
	cmd := newConverterCommand()
	flag := cmd.Flags().Lookup("exclude-bones")
	if flag == nil {
		t.Fatal("--exclude-bones flag not found")
	}
}

func TestNewConverterCommand_Flags_Recursive(t *testing.T) {
	cmd := newConverterCommand()
	flag := cmd.Flags().Lookup("recursive")
	if flag == nil {
		t.Fatal("--recursive flag not found")
	}
	if flag.Shorthand != "r" {
		t.Errorf("--recursive shorthand = %q, want %q", flag.Shorthand, "r")
	}
}

func TestNewConverterCommand_Flags_Glob(t *testing.T) {
	cmd := newConverterCommand()
	flag := cmd.Flags().Lookup("glob")
	if flag == nil {
		t.Fatal("--glob flag not found")
	}
	if flag.Shorthand != "g" {
		t.Errorf("--glob shorthand = %q, want %q", flag.Shorthand, "g")
	}
}

func TestNewConverterCommand_Flags_Validate(t *testing.T) {
	cmd := newConverterCommand()
	flag := cmd.Flags().Lookup("validate")
	if flag == nil {
		t.Fatal("--validate flag not found")
	}
}

func TestNewConverterCommand_RunE_Set(t *testing.T) {
	cmd := newConverterCommand()
	if cmd.RunE == nil {
		t.Fatal("RunE is not set")
	}
}

// ========================================
// Test getFileFormat - Format Detection
// ========================================

func TestGetFileFormat_EchoReplay(t *testing.T) {
	format := getFileFormat("test.echoreplay")
	if format != "echoreplay" {
		t.Errorf("getFileFormat(\"test.echoreplay\") = %q, want %q", format, "echoreplay")
	}
}

func TestGetFileFormat_Nevrcap(t *testing.T) {
	format := getFileFormat("test.nevrcap")
	if format != "nevrcap" {
		t.Errorf("getFileFormat(\"test.nevrcap\") = %q, want %q", format, "nevrcap")
	}
}

func TestGetFileFormat_EchoReplayUppercase(t *testing.T) {
	format := getFileFormat("test.ECHOREPLAY")
	if format != "echoreplay" {
		t.Errorf("getFileFormat(\"test.ECHOREPLAY\") = %q, want %q", format, "echoreplay")
	}
}

func TestGetFileFormat_NevrcapMixedCase(t *testing.T) {
	format := getFileFormat("test.NevrCap")
	if format != "nevrcap" {
		t.Errorf("getFileFormat(\"test.NevrCap\") = %q, want %q", format, "nevrcap")
	}
}

func TestGetFileFormat_NoExtension(t *testing.T) {
	format := getFileFormat("testfile")
	if format != "unknown" {
		t.Errorf("getFileFormat(\"testfile\") = %q, want %q", format, "unknown")
	}
}

func TestGetFileFormat_OtherExtension(t *testing.T) {
	format := getFileFormat("test.txt")
	if format != "unknown" {
		t.Errorf("getFileFormat(\"test.txt\") = %q, want %q", format, "unknown")
	}
}

func TestGetFileFormat_WithPath(t *testing.T) {
	format := getFileFormat("/path/to/test.echoreplay")
	if format != "echoreplay" {
		t.Errorf("getFileFormat with path = %q, want %q", format, "echoreplay")
	}
}

// ========================================
// Test determineOutputFileForInput - Output Path Logic
// ========================================

func TestDetermineOutputFileForInput_ExplicitOutputFile(t *testing.T) {
	// Set up config
	convOutputFile = "/tmp/explicit.nevrcap"
	convOutputDir = ""
	defer func() {
		convOutputFile = ""
		convOutputDir = "./"
	}()
	
	output, err := determineOutputFileForInput("/tmp/input.echoreplay")
	if err != nil {
		t.Fatalf("determineOutputFileForInput failed: %v", err)
	}
	if output != "/tmp/explicit.nevrcap" {
		t.Errorf("output = %q, want %q", output, "/tmp/explicit.nevrcap")
	}
}

func TestDetermineOutputFileForInput_OutputDirEchoReplayToNevrcap(t *testing.T) {
	tmpDir := t.TempDir()
	convOutputFile = ""
	convOutputDir = tmpDir
	convFormat = "auto"
	defer func() {
		convOutputFile = ""
		convOutputDir = "./"
		convFormat = "auto"
	}()
	
	output, err := determineOutputFileForInput("/tmp/input.echoreplay")
	if err != nil {
		t.Fatalf("determineOutputFileForInput failed: %v", err)
	}
	
	expectedFile := filepath.Join(tmpDir, "input.nevrcap")
	if output != expectedFile {
		t.Errorf("output = %q, want %q", output, expectedFile)
	}
}

func TestDetermineOutputFileForInput_OutputDirNevrcapToEchoReplay(t *testing.T) {
	tmpDir := t.TempDir()
	convOutputFile = ""
	convOutputDir = tmpDir
	convFormat = "auto"
	defer func() {
		convOutputFile = ""
		convOutputDir = "./"
		convFormat = "auto"
	}()
	
	output, err := determineOutputFileForInput("/tmp/input.nevrcap")
	if err != nil {
		t.Fatalf("determineOutputFileForInput failed: %v", err)
	}
	
	expectedFile := filepath.Join(tmpDir, "input.echoreplay")
	if output != expectedFile {
		t.Errorf("output = %q, want %q", output, expectedFile)
	}
}

func TestDetermineOutputFileForInput_SiblingPathEchoReplay(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "test.echoreplay")
	
	// Create the input file
	f, err := os.Create(inputFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	f.Close()
	
	convOutputFile = ""
	convOutputDir = ""
	convFormat = "auto"
	defer func() {
		convOutputFile = ""
		convOutputDir = "./"
		convFormat = "auto"
	}()
	
	output, err := determineOutputFileForInput(inputFile)
	if err != nil {
		t.Fatalf("determineOutputFileForInput failed: %v", err)
	}
	
	expectedFile := filepath.Join(tmpDir, "test.nevrcap")
	if output != expectedFile {
		t.Errorf("output = %q, want %q", output, expectedFile)
	}
}

func TestDetermineOutputFileForInput_SiblingPathNevrcap(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "test.nevrcap")
	
	// Create the input file
	f, err := os.Create(inputFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	f.Close()
	
	convOutputFile = ""
	convOutputDir = ""
	convFormat = "auto"
	defer func() {
		convOutputFile = ""
		convOutputDir = "./"
		convFormat = "auto"
	}()
	
	output, err := determineOutputFileForInput(inputFile)
	if err != nil {
		t.Fatalf("determineOutputFileForInput failed: %v", err)
	}
	
	expectedFile := filepath.Join(tmpDir, "test.echoreplay")
	if output != expectedFile {
		t.Errorf("output = %q, want %q", output, expectedFile)
	}
}

func TestDetermineOutputFileForInput_ExplicitFormatNevrcap(t *testing.T) {
	tmpDir := t.TempDir()
	convOutputFile = ""
	convOutputDir = tmpDir
	convFormat = "nevrcap"
	defer func() {
		convOutputFile = ""
		convOutputDir = "./"
		convFormat = "auto"
	}()
	
	output, err := determineOutputFileForInput("/tmp/input.echoreplay")
	if err != nil {
		t.Fatalf("determineOutputFileForInput failed: %v", err)
	}
	
	expectedFile := filepath.Join(tmpDir, "input.nevrcap")
	if output != expectedFile {
		t.Errorf("output = %q, want %q", output, expectedFile)
	}
}

func TestDetermineOutputFileForInput_ExplicitFormatEchoReplay(t *testing.T) {
	tmpDir := t.TempDir()
	convOutputFile = ""
	convOutputDir = tmpDir
	convFormat = "echoreplay"
	defer func() {
		convOutputFile = ""
		convOutputDir = "./"
		convFormat = "auto"
	}()
	
	output, err := determineOutputFileForInput("/tmp/input.nevrcap")
	if err != nil {
		t.Fatalf("determineOutputFileForInput failed: %v", err)
	}
	
	expectedFile := filepath.Join(tmpDir, "input.echoreplay")
	if output != expectedFile {
		t.Errorf("output = %q, want %q", output, expectedFile)
	}
}

// ========================================
// Test discoverFiles - File Discovery
// ========================================

func TestDiscoverFiles_SingleFile(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.echoreplay")
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	f.Close()
	
	convInputFile = tmpFile
	convRecursive = false
	convGlob = ""
	defer func() {
		convInputFile = ""
		convRecursive = false
		convGlob = ""
	}()
	
	files, err := discoverFiles()
	if err != nil {
		t.Fatalf("discoverFiles failed: %v", err)
	}
	
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0] != tmpFile {
		t.Errorf("file = %q, want %q", files[0], tmpFile)
	}
}

func TestDiscoverFiles_RecursiveDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create test files
	file1 := filepath.Join(tmpDir, "test1.echoreplay")
	file2 := filepath.Join(tmpDir, "test2.nevrcap")
	
	for _, f := range []string{file1, file2} {
		file, err := os.Create(f)
		if err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		file.Close()
	}
	
	// Create subdirectory with file
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	file3 := filepath.Join(subDir, "test3.echoreplay")
	if f, err := os.Create(file3); err == nil {
		f.Close()
	}
	
	convInputFile = tmpDir
	convRecursive = true
	convGlob = ""
	defer func() {
		convInputFile = ""
		convRecursive = false
		convGlob = ""
	}()
	
	files, err := discoverFiles()
	if err != nil {
		t.Fatalf("discoverFiles failed: %v", err)
	}
	
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}
}

func TestDiscoverFiles_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	
	convInputFile = tmpDir
	convRecursive = true
	convGlob = ""
	defer func() {
		convInputFile = ""
		convRecursive = false
		convGlob = ""
	}()
	
	files, err := discoverFiles()
	if err != nil {
		t.Fatalf("discoverFiles failed: %v", err)
	}
	
	if len(files) != 0 {
		t.Errorf("expected 0 files in empty directory, got %d", len(files))
	}
}

func TestDiscoverFiles_GlobPattern(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create test files
	file1 := filepath.Join(tmpDir, "test1.echoreplay")
	file2 := filepath.Join(tmpDir, "test2.nevrcap")
	file3 := filepath.Join(tmpDir, "other.txt")
	
	for _, f := range []string{file1, file2, file3} {
		file, err := os.Create(f)
		if err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		file.Close()
	}
	
	convInputFile = tmpDir
	convRecursive = true
	convGlob = "*.echoreplay"
	defer func() {
		convInputFile = ""
		convRecursive = false
		convGlob = ""
	}()
	
	files, err := discoverFiles()
	if err != nil {
		t.Fatalf("discoverFiles failed: %v", err)
	}
	
	if len(files) != 1 {
		t.Errorf("expected 1 file matching glob, got %d", len(files))
	}
	if len(files) > 0 && filepath.Base(files[0]) != "test1.echoreplay" {
		t.Errorf("expected test1.echoreplay, got %s", filepath.Base(files[0]))
	}
}
