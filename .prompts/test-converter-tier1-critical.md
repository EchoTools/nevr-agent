# Test Implementation: Converter Tier 1 (CRITICAL Functions)

## Context

This prompt is for implementing comprehensive test coverage for the 5 most critical functions in `cmd/agent/converter.go`. These functions have **0% test coverage** and represent core business logic for the converter command, including the newly implemented recursive/glob search and round-trip validation features.

**Current Coverage**: 0.0% (0 of 933 lines covered)
**Target Coverage**: 80%+ for these 5 functions
**Test Framework**: Standard Go `testing` package (NO testify/assert library)
**Test Patterns**: Based on `cmd/agent/smoke_test.go` - use `exec.Command("go", "run", ".", ...)` for CLI testing

## Functions to Test (Priority Order)

### 1. `runConverter(ctx context.Context, cmd *cobra.Command, cfg *config.ConverterConfig) error`
**Location**: Lines 140-301
**Complexity**: HIGH (162 lines, 15+ branches)
**Why Critical**: Main orchestrator - calls all other functions, handles all execution paths

**Function Signature**:
```go
func runConverter(ctx context.Context, cmd *cobra.Command, cfg *config.ConverterConfig) error
```

**Key Logic**:
- Lines 143-168: File discovery (single file vs recursive/glob)
- Lines 170-193: Progress bar setup (varies by file count)
- Lines 195-297: File processing loop with error handling
- Lines 230-249: Round-trip validation flow
- Lines 251-270: Success/failure tracking and reporting

**Test Cases Needed** (~100 cases):

#### A. Single File Conversion (20 cases)
- ✅ Convert single .echoreplay → .nevrcap (success)
- ✅ Convert single .nevrcap → .echoreplay (success)
- ✅ Convert with verbose flag (check output format)
- ✅ Convert with overwrite flag (existing output file)
- ✅ Convert without overwrite flag (existing output file - should fail)
- ✅ Convert with exclude-bones flag
- ✅ Convert with explicit output file
- ✅ Convert with output directory
- ✅ Convert non-existent input file (should fail)
- ✅ Convert corrupted input file (should fail gracefully)
- ✅ Convert with context cancellation (SIGINT simulation)
- ✅ Convert zero-byte file (edge case)
- ✅ Convert file with no frames (edge case)
- ✅ Convert file with 1 frame (edge case)
- ✅ Convert file with 1,000,000+ frames (large file)
- ✅ Convert with insufficient disk space (error handling)
- ✅ Convert with read-only output directory (permission error)
- ✅ Convert same format with auto-detection (should trigger convertSameFormat)
- ✅ Convert with invalid output path (directory doesn't exist)
- ✅ Convert with progress bar for long operation (>1 second)

#### B. Recursive Directory Conversion (25 cases)
- ✅ Recursive flag with directory containing .echoreplay files
- ✅ Recursive flag with directory containing .nevrcap files
- ✅ Recursive flag with mixed format directory
- ✅ Recursive flag with nested subdirectories (3+ levels deep)
- ✅ Recursive flag with empty directory (should succeed with 0 files)
- ✅ Recursive flag with directory containing no matching files
- ✅ Recursive flag with output-dir specified
- ✅ Recursive flag without output-dir (should fail - validated in config)
- ✅ Recursive flag with non-existent directory (should fail)
- ✅ Recursive flag with file as input (should fail - validated in config)
- ✅ Recursive flag with symlinked directories (follow symlinks)
- ✅ Recursive flag with circular symlinks (should not hang)
- ✅ Recursive flag with 100+ files (performance test)
- ✅ Recursive flag with mix of valid/invalid files (partial success)
- ✅ Recursive flag with overwrite flag (mass overwrite)
- ✅ Recursive flag without overwrite flag (skip existing files)
- ✅ Recursive flag with verbose output (check progress tracking)
- ✅ Recursive flag with context cancellation mid-processing
- ✅ Recursive flag with read-only files (permission errors)
- ✅ Recursive flag with output directory creation (doesn't exist)
- ✅ Recursive flag with exclude-bones (applies to all files)
- ✅ Recursive flag + glob pattern combined
- ✅ Recursive flag with duplicate filenames in different directories
- ✅ Recursive flag with files being modified during scan (race condition)
- ✅ Recursive flag error summary (X succeeded, Y failed)

#### C. Glob Pattern Conversion (25 cases)
- ✅ Glob pattern "*.echoreplay" in current directory
- ✅ Glob pattern "**/*.echoreplay" (recursive glob)
- ✅ Glob pattern "test_*.nevrcap" (prefix match)
- ✅ Glob pattern "*_recording.echoreplay" (suffix match)
- ✅ Glob pattern with character class "[abc]*.echoreplay"
- ✅ Glob pattern with range "[0-9]*.nevrcap"
- ✅ Glob pattern with negation "[!test]*.echoreplay"
- ✅ Glob pattern with brace expansion "{foo,bar}*.echoreplay"
- ✅ Glob pattern matching 0 files (should succeed with 0 files)
- ✅ Glob pattern matching 1 file (single match)
- ✅ Glob pattern matching 100+ files (performance test)
- ✅ Glob pattern with spaces in filename "test *.echoreplay"
- ✅ Glob pattern with special chars "test[1].echoreplay"
- ✅ Glob pattern with absolute path "/tmp/*.echoreplay"
- ✅ Glob pattern with relative path "../testdata/*.echoreplay"
- ✅ Glob pattern invalid syntax (should fail gracefully)
- ✅ Glob pattern with output-dir specified
- ✅ Glob pattern without output-dir (should fail - validated in config)
- ✅ Glob pattern with overwrite flag
- ✅ Glob pattern with exclude-bones
- ✅ Glob pattern + recursive flag combined
- ✅ Glob pattern with verbose output
- ✅ Glob pattern with context cancellation
- ✅ Glob pattern matching both .echoreplay and .nevrcap (mixed formats)
- ✅ Glob pattern error summary (X succeeded, Y failed)

#### D. Validation Mode (15 cases)
- ✅ Validate flag with single .echoreplay file (round-trip success)
- ✅ Validate flag with single .nevrcap file (round-trip success)
- ✅ Validate flag with corrupted file (validation should fail)
- ✅ Validate flag with file missing frames (data loss detection)
- ✅ Validate flag with file with extra frames (data addition detection)
- ✅ Validate flag with modified frame data (data corruption detection)
- ✅ Validate flag with verbose output (show comparison details)
- ✅ Validate flag with exclude-bones (bones excluded from comparison)
- ✅ Validate flag + recursive flag (should fail - invalid combo in config)
- ✅ Validate flag + glob flag (should fail - invalid combo in config)
- ✅ Validate flag with context cancellation during validation
- ✅ Validate flag with large file (performance test)
- ✅ Validate flag with zero-frame file (edge case)
- ✅ Validate flag with single-frame file (edge case)
- ✅ Validate flag success message format (verify output)

#### E. Error Handling & Edge Cases (15 cases)
- ✅ Multiple conversion failures (aggregate error reporting)
- ✅ Disk full during conversion (graceful failure)
- ✅ Network filesystem timeout (I/O error handling)
- ✅ File deleted between discovery and conversion (race condition)
- ✅ Output file locked by another process (write error)
- ✅ Conversion interrupted by signal (cleanup behavior)
- ✅ Progress bar with terminal width detection failure
- ✅ Progress bar with non-TTY output (should disable)
- ✅ Zero-length progress bar (edge case)
- ✅ Progress bar update frequency (performance check)
- ✅ Error message formatting (user-friendly output)
- ✅ Success rate calculation (0%, 50%, 100%)
- ✅ Memory usage with large batch (performance test)
- ✅ Concurrent file access (file locking behavior)
- ✅ Path normalization (Windows vs Unix paths)

---

### 2. `convertFile(ctx context.Context, cfg *config.ConverterConfig, inputFile, outputFile string) error`
**Location**: Lines 303-527
**Complexity**: HIGH (225 lines, 20+ branches)
**Why Critical**: Core conversion logic - handles format detection, codec initialization, frame processing

**Function Signature**:
```go
func convertFile(ctx context.Context, cfg *config.ConverterConfig, inputFile, outputFile string) error
```

**Key Logic**:
- Lines 308-333: Format detection (auto vs explicit)
- Lines 335-372: Source reader initialization (EchoReplay vs Nevrcap)
- Lines 374-408: Destination writer initialization
- Lines 410-436: Header frame conversion
- Lines 438-479: Frame-by-frame conversion loop
- Lines 481-527: Metadata updates and finalization

**Test Cases Needed** (~80 cases):

#### A. Format Detection (15 cases)
- ✅ Auto-detect .echoreplay extension
- ✅ Auto-detect .nevrcap extension
- ✅ Auto-detect .ECHOREPLAY (uppercase extension)
- ✅ Auto-detect .NevrCap (mixed case extension)
- ✅ Auto-detect no extension + file magic bytes (EchoReplay)
- ✅ Auto-detect no extension + file magic bytes (Nevrcap)
- ✅ Auto-detect unknown extension with valid content
- ✅ Auto-detect empty file (should fail)
- ✅ Auto-detect corrupted magic bytes (should fail)
- ✅ Explicit format "echoreplay" overrides auto-detection
- ✅ Explicit format "nevrcap" overrides auto-detection
- ✅ Explicit format "auto" behaves like auto-detection
- ✅ Explicit format invalid value (validated in config)
- ✅ Format mismatch (explicit vs actual) - should fail gracefully
- ✅ Format detection with symlinked files

#### B. EchoReplay → Nevrcap Conversion (20 cases)
- ✅ Convert valid .echoreplay file (basic success)
- ✅ Convert .echoreplay with header frame only
- ✅ Convert .echoreplay with 1 data frame
- ✅ Convert .echoreplay with 10,000 frames
- ✅ Convert .echoreplay with 1,000,000 frames (large file)
- ✅ Convert .echoreplay with exclude-bones flag
- ✅ Convert .echoreplay without exclude-bones flag
- ✅ Convert .echoreplay with all frame types (data, header, metadata)
- ✅ Convert .echoreplay with sparse frames (gaps in sequence)
- ✅ Convert .echoreplay with duplicate frame IDs (edge case)
- ✅ Convert .echoreplay with missing header frame (should handle gracefully)
- ✅ Convert .echoreplay with corrupted frame data
- ✅ Convert .echoreplay with malformed JSON in frame
- ✅ Convert .echoreplay with very large frame (>10MB)
- ✅ Convert .echoreplay with zero-byte frame
- ✅ Convert .echoreplay with context cancellation mid-conversion
- ✅ Convert .echoreplay with verbose output (log each frame)
- ✅ Convert .echoreplay with output file already exists (overwrite=true)
- ✅ Convert .echoreplay with output file already exists (overwrite=false)
- ✅ Convert .echoreplay with invalid output path (should fail early)

#### C. Nevrcap → EchoReplay Conversion (20 cases)
- ✅ Convert valid .nevrcap file (basic success)
- ✅ Convert .nevrcap with header frame only
- ✅ Convert .nevrcap with 1 data frame
- ✅ Convert .nevrcap with 10,000 frames
- ✅ Convert .nevrcap with 1,000,000 frames (large file)
- ✅ Convert .nevrcap with exclude-bones flag
- ✅ Convert .nevrcap without exclude-bones flag
- ✅ Convert .nevrcap with all frame types
- ✅ Convert .nevrcap with sparse frames
- ✅ Convert .nevrcap with duplicate frame IDs
- ✅ Convert .nevrcap with missing header frame
- ✅ Convert .nevrcap with corrupted frame data
- ✅ Convert .nevrcap with malformed protobuf
- ✅ Convert .nevrcap with very large frame (>10MB)
- ✅ Convert .nevrcap with zero-byte frame
- ✅ Convert .nevrcap with context cancellation mid-conversion
- ✅ Convert .nevrcap with verbose output
- ✅ Convert .nevrcap with output file already exists (overwrite=true)
- ✅ Convert .nevrcap with output file already exists (overwrite=false)
- ✅ Convert .nevrcap with invalid output path

#### D. Frame Processing (15 cases)
- ✅ Header frame conversion (EchoReplay → Nevrcap)
- ✅ Header frame conversion (Nevrcap → EchoReplay)
- ✅ Header frame with missing fields (should handle gracefully)
- ✅ Header frame with extra fields (should preserve)
- ✅ Data frame conversion with bones included
- ✅ Data frame conversion with bones excluded
- ✅ Data frame with nil BoneFrames field
- ✅ Data frame with empty BoneFrames slice
- ✅ Data frame with 100+ bone frames (large bone data)
- ✅ Frame counter increments correctly (no skips)
- ✅ Frame counter with gaps in source data
- ✅ Frame metadata preservation (timestamps, IDs)
- ✅ Frame ordering preservation (sequential processing)
- ✅ Frame error handling (skip corrupt frames vs fail)
- ✅ Frame progress tracking (percentage calculation)

#### E. Resource Management (10 cases)
- ✅ File handles closed on success
- ✅ File handles closed on error
- ✅ File handles closed on context cancellation
- ✅ Temporary file cleanup on error
- ✅ Partial output file cleanup on error
- ✅ Memory usage with large frames (no leaks)
- ✅ Memory usage with many small frames (no leaks)
- ✅ Concurrent conversions (file locking)
- ✅ Reader/Writer initialization errors
- ✅ Reader/Writer finalization errors

---

### 3. `discoverFiles(cfg *config.ConverterConfig) ([]string, error)`
**Location**: Lines 629-682
**Complexity**: MEDIUM (54 lines, 8+ branches)
**Why Critical**: **NEW FEATURE** - implements recursive and glob search, core to batch operations

**Function Signature**:
```go
func discoverFiles(cfg *config.ConverterConfig) ([]string, error)
```

**Key Logic**:
- Lines 631-634: Single file mode (InputFile specified)
- Lines 636-678: Recursive mode with glob filtering
- Lines 649-654: Directory walking with WalkDir
- Lines 656-670: File filtering (extension, glob pattern)

**Test Cases Needed** (~40 cases):

#### A. Single File Discovery (10 cases)
- ✅ InputFile specified, Recursive=false, Glob="" (return single file)
- ✅ InputFile specified with absolute path
- ✅ InputFile specified with relative path
- ✅ InputFile specified with symlink
- ✅ InputFile specified with ~/ home directory expansion (if supported)
- ✅ InputFile non-existent (should fail)
- ✅ InputFile is directory (should fail - handled by validation)
- ✅ InputFile with .echoreplay extension
- ✅ InputFile with .nevrcap extension
- ✅ InputFile with no extension

#### B. Recursive Discovery (15 cases)
- ✅ Recursive=true, directory with .echoreplay files only
- ✅ Recursive=true, directory with .nevrcap files only
- ✅ Recursive=true, directory with mixed .echoreplay and .nevrcap files
- ✅ Recursive=true, empty directory (return empty slice)
- ✅ Recursive=true, directory with no matching extensions (return empty slice)
- ✅ Recursive=true, nested subdirectories 1 level deep
- ✅ Recursive=true, nested subdirectories 3+ levels deep
- ✅ Recursive=true, nested subdirectories with files at multiple levels
- ✅ Recursive=true, directory with hidden files (.echoreplay)
- ✅ Recursive=true, directory with symlinked files (should follow)
- ✅ Recursive=true, directory with symlinked directories (should follow)
- ✅ Recursive=true, directory with circular symlinks (should not hang)
- ✅ Recursive=true, directory with permission errors (skip inaccessible)
- ✅ Recursive=true, directory with 100+ files (performance test)
- ✅ Recursive=true, file ordering (should be deterministic)

#### C. Glob Filtering (15 cases)
- ✅ Glob="*.echoreplay" matches .echoreplay files only
- ✅ Glob="*.nevrcap" matches .nevrcap files only
- ✅ Glob="test_*.echoreplay" matches prefix pattern
- ✅ Glob="*_recording.nevrcap" matches suffix pattern
- ✅ Glob="*session[0-9].echoreplay" matches range pattern
- ✅ Glob="*{foo,bar}*.echoreplay" matches brace expansion
- ✅ Glob="" matches all .echoreplay and .nevrcap files (no filter)
- ✅ Glob="*.echoreplay" with Recursive=true (applies to all subdirs)
- ✅ Glob with no matches (return empty slice)
- ✅ Glob with 1 match (return single file)
- ✅ Glob with invalid syntax (should fail gracefully or match nothing)
- ✅ Glob with spaces in pattern "test *.echoreplay"
- ✅ Glob with special characters "test[1].echoreplay"
- ✅ Glob case sensitivity (platform-dependent behavior)
- ✅ Glob matching across multiple subdirectories

---

### 4. `validateRoundTrip(ctx context.Context, cfg *config.ConverterConfig, originalFile, convertedFile string) error`
**Location**: Lines 731-788
**Complexity**: MEDIUM (58 lines, 6+ branches)
**Why Critical**: **NEW FEATURE** - validates data integrity, ensures no data loss during conversion

**Function Signature**:
```go
func validateRoundTrip(ctx context.Context, cfg *config.ConverterConfig, originalFile, convertedFile string) error
```

**Key Logic**:
- Lines 738-745: Read original file raw JSON frames
- Lines 747-755: Convert back to original format (temp file)
- Lines 757-760: Read converted file raw JSON frames
- Lines 762-768: Compare frame counts
- Lines 770-782: Frame-by-frame comparison

**Test Cases Needed** (~35 cases):

#### A. Successful Validation (10 cases)
- ✅ Round-trip .echoreplay → .nevrcap → .echoreplay (identical)
- ✅ Round-trip .nevrcap → .echoreplay → .nevrcap (identical)
- ✅ Round-trip with single frame (minimal test)
- ✅ Round-trip with 10,000 frames (medium file)
- ✅ Round-trip with 100,000 frames (large file)
- ✅ Round-trip with exclude-bones flag (bones excluded from comparison)
- ✅ Round-trip without exclude-bones flag (bones included)
- ✅ Round-trip with verbose output (show frame-by-frame progress)
- ✅ Round-trip with header frame only (edge case)
- ✅ Round-trip with mixed frame types (header, data, metadata)

#### B. Validation Failures (15 cases)
- ✅ Frame count mismatch (original has more frames)
- ✅ Frame count mismatch (converted has more frames)
- ✅ Frame data mismatch (field value changed)
- ✅ Frame data mismatch (field added)
- ✅ Frame data mismatch (field removed)
- ✅ Frame data mismatch (nested object changed)
- ✅ Frame data mismatch (array length changed)
- ✅ Frame data mismatch (array order changed)
- ✅ Frame data mismatch (numeric precision loss)
- ✅ Frame data mismatch (string encoding issue)
- ✅ Frame data mismatch (boolean type change)
- ✅ Frame data mismatch (null vs empty string)
- ✅ Frame data mismatch (timestamp format change)
- ✅ Frame metadata mismatch (timestamp, ID)
- ✅ Validation error message format (clear, actionable)

#### C. Edge Cases & Errors (10 cases)
- ✅ Original file corrupted (read error)
- ✅ Converted file corrupted (read error)
- ✅ Temporary file creation fails (disk full)
- ✅ Temporary file write fails (I/O error)
- ✅ Temporary file cleanup on success
- ✅ Temporary file cleanup on failure
- ✅ Context cancellation during validation
- ✅ Zero-frame file validation (edge case)
- ✅ Validation with non-existent original file
- ✅ Validation with non-existent converted file

---

### 5. `readRawJSONFrames(ctx context.Context, inputFile, format string, excludeBones bool) ([]map[string]interface{}, error)`
**Location**: Lines 796-870
**Complexity**: MEDIUM (75 lines, 8+ branches)
**Why Critical**: **NEW FEATURE** - extracts raw JSON for validation, must preserve ALL fields

**Function Signature**:
```go
func readRawJSONFrames(ctx context.Context, inputFile, format string, excludeBones bool) ([]map[string]interface{}, error)
```

**Key Logic**:
- Lines 801-829: Reader initialization (EchoReplay vs Nevrcap)
- Lines 831-863: Frame reading loop with JSON marshaling
- Lines 849-856: Bone frame exclusion logic
- Lines 864-870: Cleanup and return

**Test Cases Needed** (~30 cases):

#### A. EchoReplay Reading (10 cases)
- ✅ Read .echoreplay file with 1 frame
- ✅ Read .echoreplay file with 100 frames
- ✅ Read .echoreplay file with 10,000 frames
- ✅ Read .echoreplay file with header frame only
- ✅ Read .echoreplay file with mixed frame types
- ✅ Read .echoreplay file with bones excluded
- ✅ Read .echoreplay file with bones included
- ✅ Read .echoreplay file with corrupted frame (should fail)
- ✅ Read .echoreplay file with malformed JSON (should fail)
- ✅ Read .echoreplay file with context cancellation

#### B. Nevrcap Reading (10 cases)
- ✅ Read .nevrcap file with 1 frame
- ✅ Read .nevrcap file with 100 frames
- ✅ Read .nevrcap file with 10,000 frames
- ✅ Read .nevrcap file with header frame only
- ✅ Read .nevrcap file with mixed frame types
- ✅ Read .nevrcap file with bones excluded
- ✅ Read .nevrcap file with bones included
- ✅ Read .nevrcap file with corrupted frame (should fail)
- ✅ Read .nevrcap file with malformed protobuf (should fail)
- ✅ Read .nevrcap file with context cancellation

#### C. Data Preservation (10 cases)
- ✅ All JSON fields preserved (no loss)
- ✅ Nested objects preserved
- ✅ Arrays preserved (order and content)
- ✅ Numeric values preserved (int, float, scientific notation)
- ✅ String values preserved (unicode, escapes)
- ✅ Boolean values preserved
- ✅ Null values preserved
- ✅ Empty objects preserved {}
- ✅ Empty arrays preserved []
- ✅ Large JSON objects preserved (>1MB)

---

## Test Data Requirements

### A. Create Test Fixtures in `/home/andrew/src/nevr-agent/testdata/converter/`
```
testdata/converter/
├── valid_single_frame.echoreplay         # 1 frame EchoReplay file
├── valid_single_frame.nevrcap            # 1 frame Nevrcap file
├── valid_small.echoreplay                # ~100 frames
├── valid_small.nevrcap                   # ~100 frames
├── valid_medium.echoreplay               # ~10,000 frames
├── valid_medium.nevrcap                  # ~10,000 frames
├── valid_large.echoreplay                # ~100,000 frames (optional)
├── valid_large.nevrcap                   # ~100,000 frames (optional)
├── valid_no_bones.echoreplay             # File with BoneFrames=nil
├── valid_with_bones.echoreplay           # File with populated BoneFrames
├── header_only.echoreplay                # Only header frame, no data
├── header_only.nevrcap                   # Only header frame, no data
├── corrupted_header.echoreplay           # Corrupted magic bytes
├── corrupted_frame.echoreplay            # Valid header, corrupted frame data
├── malformed_json.echoreplay             # Frame with invalid JSON
├── empty.echoreplay                      # Zero-byte file
├── recursive/                            # Directory for recursive tests
│   ├── file1.echoreplay
│   ├── file2.echoreplay
│   ├── subdir1/
│   │   ├── file3.echoreplay
│   │   └── file4.nevrcap
│   └── subdir2/
│       └── subdir3/
│           └── file5.echoreplay
├── glob/                                 # Directory for glob tests
│   ├── test_session1.echoreplay
│   ├── test_session2.echoreplay
│   ├── prod_recording_001.nevrcap
│   ├── prod_recording_002.nevrcap
│   └── other_file.txt                    # Non-matching file
└── validation/                           # Files for validation tests
    ├── original.echoreplay               # Reference file
    ├── identical.echoreplay              # Exact copy
    ├── missing_frame.echoreplay          # One frame removed
    ├── extra_frame.echoreplay            # One frame added
    └── modified_field.echoreplay         # One field value changed
```

### B. Mocking Requirements

**External Dependencies to Mock**:
1. **File System Operations**:
   - Use `os.CreateTemp()` for temporary directories
   - Use `filepath.Join()` for cross-platform paths
   - Mock disk full errors with custom `io.Writer`

2. **nevr-capture Library** (DO NOT MOCK - use real library):
   - `codecs.NewEchoReplayReader()`
   - `codecs.NewNevrCapReader()`
   - `conversion.ConvertEchoReplayToNevrcap()`
   - `conversion.ConvertNevrcapToEchoReplay()`
   - **Rationale**: Integration tests needed to verify actual conversion correctness

3. **Context Cancellation**:
   - Create context with timeout: `ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)`
   - Cancel during execution to test cleanup

4. **Progress Bar** (optional to mock):
   - Replace `os.Stdout` with `bytes.Buffer` to capture output
   - Use `TERM=dumb` environment variable to disable interactive features

### C. Helper Functions to Create

```go
// Helper: Create temporary test file with N frames
func createTestFile(t *testing.T, format string, frameCount int, includeBones bool) string {
    // Implementation needed
}

// Helper: Compare two JSON frame slices (for validation tests)
func compareJSONFrames(t *testing.T, expected, actual []map[string]interface{}) {
    // Implementation needed
}

// Helper: Create temporary directory structure for recursive tests
func createTestDirectory(t *testing.T, structure map[string]string) string {
    // structure: map[relativePath]fileContent
    // Implementation needed
}

// Helper: Count frames in a file (for verification)
func countFramesInFile(t *testing.T, filePath, format string) int {
    // Implementation needed
}

// Helper: Modify a frame in a file (for validation failure tests)
func modifyFrameInFile(t *testing.T, filePath, format string, frameIndex int, fieldPath string, newValue interface{}) {
    // Implementation needed
}
```

---

## Test File Structure

Create: `/home/andrew/src/nevr-agent/cmd/agent/converter_critical_test.go`

```go
package agent

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    "time"
    
    "github.com/nevrtech/nevr-agent/internal/config"
)

// Test runConverter - Single File Conversion
func TestRunConverter_SingleFile_EchoReplayToNevrcap(t *testing.T) {
    // TODO: Implement
}

func TestRunConverter_SingleFile_NevrCapToEchoReplay(t *testing.T) {
    // TODO: Implement
}

// Test runConverter - Recursive
func TestRunConverter_Recursive_ValidDirectory(t *testing.T) {
    // TODO: Implement
}

// Test runConverter - Glob
func TestRunConverter_Glob_PatternMatching(t *testing.T) {
    // TODO: Implement
}

// Test runConverter - Validation
func TestRunConverter_Validate_Success(t *testing.T) {
    // TODO: Implement
}

// Test convertFile - Format Detection
func TestConvertFile_FormatDetection_AutoDetect(t *testing.T) {
    // TODO: Implement
}

// Test convertFile - EchoReplay to Nevrcap
func TestConvertFile_EchoReplayToNevrcap_BasicSuccess(t *testing.T) {
    // TODO: Implement
}

// Test convertFile - Nevrcap to EchoReplay
func TestConvertFile_NevrcapToEchoReplay_BasicSuccess(t *testing.T) {
    // TODO: Implement
}

// Test discoverFiles - Single File
func TestDiscoverFiles_SingleFile(t *testing.T) {
    // TODO: Implement
}

// Test discoverFiles - Recursive
func TestDiscoverFiles_Recursive_NestedDirectories(t *testing.T) {
    // TODO: Implement
}

// Test discoverFiles - Glob
func TestDiscoverFiles_Glob_PatternMatching(t *testing.T) {
    // TODO: Implement
}

// Test validateRoundTrip - Success
func TestValidateRoundTrip_Success_EchoReplay(t *testing.T) {
    // TODO: Implement
}

// Test validateRoundTrip - Failure
func TestValidateRoundTrip_Failure_FrameCountMismatch(t *testing.T) {
    // TODO: Implement
}

// Test readRawJSONFrames - EchoReplay
func TestReadRawJSONFrames_EchoReplay_ValidFile(t *testing.T) {
    // TODO: Implement
}

// Test readRawJSONFrames - Nevrcap
func TestReadRawJSONFrames_Nevrcap_ValidFile(t *testing.T) {
    // TODO: Implement
}

// Test readRawJSONFrames - Data Preservation
func TestReadRawJSONFrames_DataPreservation_AllFields(t *testing.T) {
    // TODO: Implement
}

// Helper functions
func createTestFile(t *testing.T, format string, frameCount int, includeBones bool) string {
    // TODO: Implement
    return ""
}

func createTestDirectory(t *testing.T, structure map[string]string) string {
    // TODO: Implement
    return ""
}

func countFramesInFile(t *testing.T, filePath, format string) int {
    // TODO: Implement
    return 0
}
```

---

## Acceptance Criteria

1. **Coverage Target**: 80%+ line coverage for all 5 functions
2. **Test Count**: ~285 test cases implemented (100 + 80 + 40 + 35 + 30)
3. **All Edge Cases Covered**: Including error paths, cancellation, resource cleanup
4. **Test Fixtures Created**: Complete `testdata/converter/` directory structure
5. **Helper Functions Implemented**: All 5 helper functions created and documented
6. **Tests Pass**: `go test ./cmd/agent -v -run TestRunConverter|TestConvertFile|TestDiscoverFiles|TestValidateRoundTrip|TestReadRawJSONFrames` exits 0
7. **No Testify**: All assertions use standard `testing` package (`if got != want { t.Errorf(...) }`)
8. **Documentation**: Each test has clear comments explaining what it tests and why

---

## Notes for Implementation Agent

- **Do NOT use testify/assert library** - Use standard `if got != want { t.Errorf() }` assertions
- **Follow existing test patterns** from `cmd/agent/smoke_test.go`
- **Use real nevr-capture library** - Do not mock codec/conversion functions
- **Create comprehensive test fixtures** - Quality test data is critical
- **Test cleanup** - All tests must clean up temporary files/directories
- **Parallel execution** - Use `t.Parallel()` where safe (no shared state)
- **Context usage** - All tests should use `context.WithTimeout()` to prevent hangs
- **Error messages** - Use descriptive `t.Errorf()` messages with expected vs actual values
- **Subtests** - Use `t.Run()` for logical grouping of related test cases

---

## Estimated Effort

- **Test Fixture Creation**: 2-3 hours (need to generate valid .echoreplay/.nevrcap files)
- **Helper Functions**: 1-2 hours
- **Test Implementation**: 8-12 hours (285 test cases)
- **Debugging & Refinement**: 2-4 hours
- **Total**: 13-21 hours

**Priority**: CRITICAL - Start here before other tiers
