# Test Implementation: Converter Tier 3 (MEDIUM Priority Functions)

## Context

This prompt is for implementing test coverage for 5 MEDIUM priority functions in `cmd/agent/converter.go`. These functions are utility/helper functions that support the core converter functionality but have lower complexity and criticality than Tier 1 and Tier 2 functions.

**Current Coverage**: 0.0% (0 of 933 lines covered)
**Target Coverage**: 70%+ for these 5 functions (lower bar than Tier 1/2)
**Test Framework**: Standard Go `testing` package (NO testify/assert library)
**Prerequisite**: Tier 1 and Tier 2 tests should be prioritized first

## Functions to Test (Priority Order)

### 1. `convertSameFormat(inputFile, outputFile, format string) error`
**Location**: Lines 569-627
**Complexity**: LOW (59 lines, simple file copy)
**Why Medium Priority**: Edge case handler, rarely executed in normal operation

**Function Signature**:
```go
func convertSameFormat(inputFile, outputFile, format string) error
```

**Key Logic**:
- Lines 572-588: Format-specific instruction messages (helpful error messaging)
- Lines 590-625: Simple file copy operation (io.Copy)

**Test Cases Needed** (~15 cases):

#### A. Format Messages (3 cases)
- ✅ format="echoreplay" → Returns error with "already in echoreplay format" message
- ✅ format="nevrcap" → Returns error with "already in nevrcap format" message
- ✅ format="auto" → Returns error with format-specific message

#### B. File Copy Operation (8 cases)
- ✅ Copy small file (1KB) successfully
- ✅ Copy medium file (1MB) successfully
- ✅ Copy large file (100MB) successfully
- ✅ Copy zero-byte file (edge case)
- ✅ Copy file with spaces in name "my file.echoreplay"
- ✅ Copy file with unicode name "テスト.echoreplay"
- ✅ Verify output file has identical content (checksum)
- ✅ Verify output file has identical permissions

#### C. Error Handling (4 cases)
- ✅ Input file doesn't exist (fail gracefully)
- ✅ Input file not readable (permission error)
- ✅ Output file parent directory doesn't exist (fail)
- ✅ Output file parent directory not writable (permission error)

---

### 2. `countFrames(ctx context.Context, inputFile, format string) (int, error)`
**Location**: Lines 114-138
**Complexity**: LOW (25 lines, simple counter)
**Why Medium Priority**: Used for progress bar initialization, non-critical to conversion correctness

**Function Signature**:
```go
func countFrames(ctx context.Context, inputFile, format string) (int, error)
```

**Key Logic**:
- Lines 118-124: Reader initialization (EchoReplay vs Nevrcap)
- Lines 126-133: Frame counting loop with context cancellation support

**Test Cases Needed** (~12 cases):

#### A. EchoReplay Counting (4 cases)
- ✅ Count frames in .echoreplay file with 0 frames (return 0)
- ✅ Count frames in .echoreplay file with 1 frame (return 1)
- ✅ Count frames in .echoreplay file with 100 frames (return 100)
- ✅ Count frames in .echoreplay file with 10,000 frames (return 10,000)

#### B. Nevrcap Counting (4 cases)
- ✅ Count frames in .nevrcap file with 0 frames (return 0)
- ✅ Count frames in .nevrcap file with 1 frame (return 1)
- ✅ Count frames in .nevrcap file with 100 frames (return 100)
- ✅ Count frames in .nevrcap file with 10,000 frames (return 10,000)

#### C. Error Handling (4 cases)
- ✅ File doesn't exist (return error)
- ✅ File corrupted (return error)
- ✅ Context cancelled mid-counting (return error)
- ✅ Invalid format specified (return error)

---

### 3. `newConverterCommand() *cobra.Command`
**Location**: Lines 27-104
**Complexity**: LOW (78 lines, command setup boilerplate)
**Why Medium Priority**: CLI interface, tested implicitly by integration tests

**Function Signature**:
```go
func newConverterCommand() *cobra.Command
```

**Key Logic**:
- Lines 29-38: Command metadata (use, short, long, args)
- Lines 40-60: Flag definitions
- Lines 62-102: RunE execution flow

**Test Cases Needed** (~20 cases):

#### A. Command Metadata (5 cases)
- ✅ Command Use is "convert [flags] <input>"
- ✅ Command Short description exists and is concise
- ✅ Command Long description exists and is detailed
- ✅ Command Args is cobra.ExactArgs(1)
- ✅ Command has parent (added to root command)

#### B. Flag Definitions (10 cases)
- ✅ --output flag exists, shorthand -o
- ✅ --output-dir flag exists, shorthand -d
- ✅ --format flag exists, shorthand -f, default "auto"
- ✅ --verbose flag exists, shorthand -v
- ✅ --overwrite flag exists
- ✅ --exclude-bones flag exists
- ✅ --recursive flag exists, shorthand -r
- ✅ --glob flag exists, shorthand -g
- ✅ --validate flag exists
- ✅ All flags have descriptions

#### C. Execution Flow (5 cases)
- ✅ RunE function is set
- ✅ RunE receives context, command, args
- ✅ RunE calls LoadConfig()
- ✅ RunE calls ValidateConverterConfig()
- ✅ RunE calls runConverter()

---

### 4. `copyFile(src, dst string) error`
**Location**: Lines 567-627 (inlined in convertSameFormat, but logically separate)
**Complexity**: LOW (simple io.Copy wrapper)
**Why Medium Priority**: Utility function, standard file copy logic

**Note**: This function appears to be inlined in `convertSameFormat()`. If extracted, test it separately. If not, these tests are covered by `TestConvertSameFormat`.

**Function Signature** (if extracted):
```go
func copyFile(src, dst string) error
```

**Test Cases Needed** (~10 cases):

#### A. Successful Copy (5 cases)
- ✅ Copy regular file (success)
- ✅ Copy empty file (success)
- ✅ Copy file with special permissions (preserve permissions)
- ✅ Verify byte-for-byte identical content
- ✅ Verify file size matches

#### B. Error Handling (5 cases)
- ✅ Source file doesn't exist (fail)
- ✅ Source file not readable (fail)
- ✅ Destination parent directory doesn't exist (fail)
- ✅ Destination not writable (fail)
- ✅ Disk full during copy (fail)

---

### 5. `getFileFormat(inputFile, specifiedFormat string) (string, error)`
**Location**: Lines 308-333 (inlined in convertFile)
**Complexity**: LOW (format detection logic)
**Why Medium Priority**: Format detection, already implicitly tested via convertFile

**Note**: This logic is inlined in `convertFile()` (lines 308-333). If extracted, test it separately. If not, these tests are covered by `TestConvertFile_FormatDetection`.

**Function Signature** (if extracted):
```go
func getFileFormat(inputFile, specifiedFormat string) (string, error)
```

**Test Cases Needed** (~15 cases):

#### A. Explicit Format (3 cases)
- ✅ specifiedFormat="echoreplay" (return "echoreplay")
- ✅ specifiedFormat="nevrcap" (return "nevrcap")
- ✅ specifiedFormat="ECHOREPLAY" (normalize to "echoreplay")

#### B. Auto-Detection by Extension (6 cases)
- ✅ inputFile="file.echoreplay", specifiedFormat="auto" (return "echoreplay")
- ✅ inputFile="file.nevrcap", specifiedFormat="auto" (return "nevrcap")
- ✅ inputFile="file.ECHOREPLAY", specifiedFormat="auto" (return "echoreplay")
- ✅ inputFile="file.NevrCap", specifiedFormat="auto" (return "nevrcap")
- ✅ inputFile="file.txt", specifiedFormat="auto" (return error)
- ✅ inputFile="file", specifiedFormat="auto" (check magic bytes)

#### C. Auto-Detection by Magic Bytes (6 cases)
- ✅ File with EchoReplay magic bytes (return "echoreplay")
- ✅ File with Nevrcap magic bytes (return "nevrcap")
- ✅ File with no magic bytes (return error)
- ✅ File with corrupted magic bytes (return error)
- ✅ Empty file (return error)
- ✅ File with wrong extension but correct magic bytes (return based on magic)

---

## Test Data Requirements

### A. Test Fixtures (Reuse from Tier 1/2)

Most fixtures can be reused from Tier 1 (CRITICAL) tests:

```
testdata/converter/
├── valid_small.echoreplay                # For countFrames, copyFile tests
├── valid_small.nevrcap                   # For countFrames, copyFile tests
├── empty.echoreplay                      # Zero frames
├── single_frame.echoreplay               # 1 frame
├── corrupted_header.echoreplay           # For error handling tests
└── no_extension                          # For format detection tests
```

### B. Mocking Requirements

**Minimal mocking needed for Tier 3**:
1. **File System**: Use `os.CreateTemp()` and `t.TempDir()` for temporary files
2. **Context**: Use `context.WithTimeout()` for cancellation tests
3. **Cobra Command**: Test command structure, not full execution (use `cmd.Execute()` in integration tests)

### C. Helper Functions (Reuse from Tier 1/2)

```go
// Reuse from Tier 1
func createTestFile(t *testing.T, format string, frameCount int, includeBones bool) string

// Reuse from Tier 1
func countFramesInFile(t *testing.T, filePath, format string) int

// New helper for file comparison
func filesAreIdentical(t *testing.T, file1, file2 string) bool {
    // Compare file sizes
    info1, err := os.Stat(file1)
    if err != nil {
        t.Fatalf("filesAreIdentical: %v", err)
    }
    info2, err := os.Stat(file2)
    if err != nil {
        t.Fatalf("filesAreIdentical: %v", err)
    }
    if info1.Size() != info2.Size() {
        return false
    }
    
    // Compare content (checksum)
    checksum1 := computeSHA256(t, file1)
    checksum2 := computeSHA256(t, file2)
    return checksum1 == checksum2
}

// Helper: Compute SHA256 checksum of file
func computeSHA256(t *testing.T, filePath string) string {
    f, err := os.Open(filePath)
    if err != nil {
        t.Fatalf("computeSHA256: %v", err)
    }
    defer f.Close()
    
    h := sha256.New()
    if _, err := io.Copy(h, f); err != nil {
        t.Fatalf("computeSHA256: %v", err)
    }
    
    return hex.EncodeToString(h.Sum(nil))
}
```

---

## Test File Structure

Create: `/home/andrew/src/nevr-agent/cmd/agent/converter_medium_test.go`

```go
package agent

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "io"
    "os"
    "testing"
    "time"
    
    "github.com/spf13/cobra"
)

// Test convertSameFormat - Format Messages
func TestConvertSameFormat_FormatMessage_EchoReplay(t *testing.T) {
    // TODO: Implement
}

// Test convertSameFormat - File Copy
func TestConvertSameFormat_Copy_SmallFile(t *testing.T) {
    // TODO: Implement
}

// Test convertSameFormat - Error Handling
func TestConvertSameFormat_Error_InputNotExists(t *testing.T) {
    // TODO: Implement
}

// Test countFrames - EchoReplay
func TestCountFrames_EchoReplay_ZeroFrames(t *testing.T) {
    // TODO: Implement
}

func TestCountFrames_EchoReplay_MultipleFrames(t *testing.T) {
    // TODO: Implement
}

// Test countFrames - Nevrcap
func TestCountFrames_Nevrcap_ZeroFrames(t *testing.T) {
    // TODO: Implement
}

func TestCountFrames_Nevrcap_MultipleFrames(t *testing.T) {
    // TODO: Implement
}

// Test countFrames - Error Handling
func TestCountFrames_Error_FileNotExists(t *testing.T) {
    // TODO: Implement
}

func TestCountFrames_Error_ContextCancelled(t *testing.T) {
    // TODO: Implement
}

// Test newConverterCommand - Metadata
func TestNewConverterCommand_Metadata_Use(t *testing.T) {
    cmd := newConverterCommand()
    if cmd.Use != "convert [flags] <input>" {
        t.Errorf("Use = %q, want %q", cmd.Use, "convert [flags] <input>")
    }
}

func TestNewConverterCommand_Metadata_Short(t *testing.T) {
    // TODO: Implement
}

// Test newConverterCommand - Flags
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

func TestNewConverterCommand_Flags_Recursive(t *testing.T) {
    // TODO: Implement
}

// Test newConverterCommand - Execution
func TestNewConverterCommand_Execution_RunESet(t *testing.T) {
    cmd := newConverterCommand()
    if cmd.RunE == nil {
        t.Fatal("RunE is not set")
    }
}

// Helper functions
func filesAreIdentical(t *testing.T, file1, file2 string) bool {
    info1, err := os.Stat(file1)
    if err != nil {
        t.Fatalf("filesAreIdentical: %v", err)
    }
    info2, err := os.Stat(file2)
    if err != nil {
        t.Fatalf("filesAreIdentical: %v", err)
    }
    if info1.Size() != info2.Size() {
        return false
    }
    
    checksum1 := computeSHA256(t, file1)
    checksum2 := computeSHA256(t, file2)
    return checksum1 == checksum2
}

func computeSHA256(t *testing.T, filePath string) string {
    f, err := os.Open(filePath)
    if err != nil {
        t.Fatalf("computeSHA256: %v", err)
    }
    defer f.Close()
    
    h := sha256.New()
    if _, err := io.Copy(h, f); err != nil {
        t.Fatalf("computeSHA256: %v", err)
    }
    
    return hex.EncodeToString(h.Sum(nil))
}
```

---

## Acceptance Criteria

1. **Coverage Target**: 70%+ line coverage for all 5 functions (lower than Tier 1/2)
2. **Test Count**: ~72 test cases implemented (15 + 12 + 20 + 10 + 15)
3. **All Key Scenarios Covered**: Focus on happy path and common errors, skip exotic edge cases
4. **Test Fixtures Reused**: Minimize new fixture creation, reuse Tier 1/2 fixtures
5. **Helper Functions Implemented**: 2 new helpers (filesAreIdentical, computeSHA256)
6. **Tests Pass**: `go test ./cmd/agent -v -run TestConvertSameFormat|TestCountFrames|TestNewConverterCommand` exits 0
7. **No Testify**: All assertions use standard `testing` package
8. **Documentation**: Each test has clear comments

---

## Notes for Implementation Agent

- **Lower Priority**: Focus on Tier 1 (CRITICAL) and Tier 2 (HIGH) first
- **Simpler Tests**: Tier 3 functions are less complex, tests can be more straightforward
- **Reuse Fixtures**: Don't create new test data if Tier 1/2 fixtures work
- **Command Testing**: For `newConverterCommand`, test structure not execution (integration tests cover execution)
- **Inlined Functions**: `copyFile` and `getFileFormat` may be inlined - if so, skip separate tests or extract functions first
- **Error Messages**: Less critical than Tier 1/2, basic error checks sufficient

---

## Estimated Effort

- **Test Fixture Creation**: 0.5 hours (mostly reuse existing)
- **Helper Functions**: 1 hour (2 new helpers)
- **Test Implementation**: 3-4 hours (72 simpler test cases)
- **Debugging & Refinement**: 1 hour
- **Total**: 5.5-6.5 hours

**Priority**: MEDIUM - Complete after Tier 1 and Tier 2
