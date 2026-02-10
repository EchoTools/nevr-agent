# Test Implementation: Converter Tier 2 (HIGH Priority Functions)

## Context

This prompt is for implementing comprehensive test coverage for 5 HIGH priority functions in `cmd/agent/converter.go`. These functions support the CRITICAL tier functions and handle JSON comparison, output path determination, and progress bar display.

**Current Coverage**: 0.0% (0 of 933 lines covered)
**Target Coverage**: 80%+ for these 5 functions
**Test Framework**: Standard Go `testing` package (NO testify/assert library)
**Prerequisite**: Tier 1 (CRITICAL) tests should be completed first

## Functions to Test (Priority Order)

### 1. `compareJSONFrames(expected, actual []map[string]interface{}, excludeBones bool) error`
**Location**: Lines 872-903
**Complexity**: MEDIUM (32 lines, 4+ branches)
**Why High Priority**: **NEW FEATURE** - Core validation logic, must detect ALL data differences

**Function Signature**:
```go
func compareJSONFrames(expected, actual []map[string]interface{}, excludeBones bool) error
```

**Key Logic**:
- Lines 874-876: Frame count comparison
- Lines 878-898: Frame-by-frame comparison loop
- Lines 881-885: Bone exclusion logic (if excludeBones=true)
- Lines 887-896: JSON normalization and comparison

**Test Cases Needed** (~50 cases):

#### A. Frame Count Comparison (8 cases)
- ✅ Equal frame counts (0 frames) - success
- ✅ Equal frame counts (1 frame) - success
- ✅ Equal frame counts (100 frames) - success
- ✅ Equal frame counts (10,000 frames) - success
- ✅ Expected has more frames (100 vs 99) - fail with clear error
- ✅ Actual has more frames (99 vs 100) - fail with clear error
- ✅ Expected empty, actual has frames - fail
- ✅ Expected has frames, actual empty - fail

#### B. Identical Frames (8 cases)
- ✅ Single frame with all fields identical
- ✅ Multiple frames all identical
- ✅ Frames with nested objects identical
- ✅ Frames with arrays identical
- ✅ Frames with mixed types (string, int, float, bool, null) identical
- ✅ Frames with empty objects {} identical
- ✅ Frames with empty arrays [] identical
- ✅ Frames with large JSON (>1MB) identical

#### C. Frame Differences - Value Changes (12 cases)
- ✅ String field value changed ("foo" → "bar")
- ✅ Integer field value changed (42 → 43)
- ✅ Float field value changed (3.14 → 3.15)
- ✅ Boolean field value changed (true → false)
- ✅ Null field changed to non-null value
- ✅ Non-null field changed to null
- ✅ Nested object field changed (obj.field: "a" → "b")
- ✅ Nested object deep change (obj.nested.field: 1 → 2)
- ✅ Array element changed ([1,2,3] → [1,2,4])
- ✅ Array length changed ([1,2,3] → [1,2])
- ✅ Array order changed ([1,2,3] → [3,2,1])
- ✅ Numeric precision difference (1.0 vs 1.00)

#### D. Frame Differences - Field Changes (8 cases)
- ✅ Field added in actual (expected: {a:1}, actual: {a:1, b:2})
- ✅ Field removed in actual (expected: {a:1, b:2}, actual: {a:1})
- ✅ Field renamed (expected: {oldName:1}, actual: {newName:1})
- ✅ Nested field added (expected: {obj:{}}, actual: {obj:{field:1}})
- ✅ Nested field removed (expected: {obj:{field:1}}, actual: {obj:{}})
- ✅ Multiple fields changed (2+ fields differ)
- ✅ Type change (expected: {a:"1"}, actual: {a:1})
- ✅ Object replaced with array (expected: {a:{}}, actual: {a:[]})

#### E. Bone Exclusion (8 cases)
- ✅ excludeBones=true, "BoneFrames" field present in both (ignored, success)
- ✅ excludeBones=true, "BoneFrames" differs (ignored, success)
- ✅ excludeBones=true, "BoneFrames" only in expected (ignored, success)
- ✅ excludeBones=true, "BoneFrames" only in actual (ignored, success)
- ✅ excludeBones=false, "BoneFrames" differs (fail)
- ✅ excludeBones=true, nested "BoneFrames" field (not top-level, should compare)
- ✅ excludeBones=true, other fields differ (fail)
- ✅ excludeBones=true, case sensitivity ("boneframes" vs "BoneFrames")

#### F. Error Messages (6 cases)
- ✅ Error message includes frame index (e.g., "frame 42")
- ✅ Error message includes field path (e.g., "session.id")
- ✅ Error message includes expected value
- ✅ Error message includes actual value
- ✅ Error message for first difference only (doesn't list all diffs)
- ✅ Error message is user-friendly and actionable

---

### 2. `compareNormalizedJSON(expected, actual map[string]interface{}, path string) error`
**Location**: Lines 905-933
**Complexity**: MEDIUM (29 lines, 8+ branches, recursive)
**Why High Priority**: **NEW FEATURE** - Recursive comparison engine, must handle all JSON types correctly

**Function Signature**:
```go
func compareNormalizedJSON(expected, actual map[string]interface{}, path string) error
```

**Key Logic**:
- Lines 907-910: Key set comparison (missing/extra keys)
- Lines 912-930: Recursive value comparison by type
- Lines 914-916: nil handling
- Lines 917-919: Map recursion
- Lines 920-922: Slice comparison (length + elements)
- Lines 924-930: Primitive value comparison

**Test Cases Needed** (~45 cases):

#### A. Key Comparison (8 cases)
- ✅ All keys present in both maps
- ✅ Key present in expected, missing in actual (fail with path)
- ✅ Key present in actual, missing in expected (fail with path)
- ✅ Multiple keys missing (report first missing)
- ✅ Empty maps (both {}) - success
- ✅ Single key map comparison
- ✅ 100+ keys comparison (performance test)
- ✅ Keys with special characters (spaces, unicode)

#### B. Nil Handling (6 cases)
- ✅ Both values nil - success
- ✅ Expected nil, actual non-nil (fail)
- ✅ Expected non-nil, actual nil (fail)
- ✅ Both values explicitly null (JSON null)
- ✅ Nested nil values (obj.field = nil)
- ✅ Array containing nil values

#### C. Map Recursion (8 cases)
- ✅ Nested object 1 level deep (identical)
- ✅ Nested object 3+ levels deep (identical)
- ✅ Nested object with difference at level 2
- ✅ Nested object with difference at level 5
- ✅ Nested empty objects ({obj: {nested: {}}})
- ✅ Nested objects with arrays inside
- ✅ Deeply nested maps (10+ levels)
- ✅ Circular reference handling (if possible in map[string]interface{})

#### D. Slice Comparison (10 cases)
- ✅ Empty slices (both []) - success
- ✅ Single element slices (identical)
- ✅ Multi-element slices (identical)
- ✅ Slice length mismatch (fail with path)
- ✅ Slice element differs at index 0
- ✅ Slice element differs at index 50
- ✅ Slice element differs at last index
- ✅ Slice of primitives (int, string, bool)
- ✅ Slice of objects (compare each object recursively)
- ✅ Slice of slices (nested arrays)

#### E. Primitive Value Comparison (8 cases)
- ✅ String values identical
- ✅ String values differ (fail with path and values)
- ✅ Integer values identical (int, int64)
- ✅ Float values identical (float32, float64)
- ✅ Float precision (3.14 vs 3.140000) - should be equal
- ✅ Boolean values identical (true/false)
- ✅ Type mismatch (string "1" vs int 1) - fail
- ✅ Zero values (0, "", false) comparison

#### F. Path Tracking (5 cases)
- ✅ Top-level field: path = "fieldName"
- ✅ Nested field: path = "parent.child"
- ✅ Deeply nested: path = "a.b.c.d.e"
- ✅ Array element: path = "array[0]"
- ✅ Nested array object: path = "array[5].field"

---

### 3. `determineOutputFileForInput(cfg *config.ConverterConfig, inputFile string) (string, error)`
**Location**: Lines 529-627
**Complexity**: MEDIUM (99 lines, 12+ branches)
**Why High Priority**: Critical for batch operations, determines output paths for all conversions

**Function Signature**:
```go
func determineOutputFileForInput(cfg *config.ConverterConfig, inputFile string) (string, error)
```

**Key Logic**:
- Lines 531-534: Explicit OutputFile specified (return as-is)
- Lines 536-575: OutputDir specified (construct output path)
- Lines 577-627: No OutputFile/OutputDir (construct sibling path)

**Test Cases Needed** (~40 cases):

#### A. Explicit OutputFile Specified (5 cases)
- ✅ cfg.OutputFile set to "/tmp/output.nevrcap" (return as-is)
- ✅ cfg.OutputFile set to relative path "output.echoreplay"
- ✅ cfg.OutputFile with no extension "output"
- ✅ cfg.OutputFile with wrong extension (e.g., input .echoreplay, output .txt)
- ✅ cfg.OutputFile with absolute path on Windows (C:\output.nevrcap)

#### B. OutputDir Specified (15 cases)
- ✅ OutputDir="/tmp", input="file.echoreplay" → "/tmp/file.nevrcap"
- ✅ OutputDir="/tmp", input="file.nevrcap" → "/tmp/file.echoreplay"
- ✅ OutputDir="./output", input="file.echoreplay" (relative path)
- ✅ OutputDir with trailing slash "/tmp/" (should normalize)
- ✅ OutputDir doesn't exist (should create or error?)
- ✅ OutputDir is read-only (permission error)
- ✅ Input file in nested directory "dir/subdir/file.echoreplay"
- ✅ Input file absolute path "/home/user/file.echoreplay"
- ✅ Input file relative path "../file.nevrcap"
- ✅ Input file with no extension "file"
- ✅ Input file with multiple dots "file.backup.echoreplay"
- ✅ Input file with spaces "my file.echoreplay" → "my file.nevrcap"
- ✅ Input file with unicode "テスト.echoreplay" → "テスト.nevrcap"
- ✅ Format=echoreplay → output gets .echoreplay extension
- ✅ Format=nevrcap → output gets .nevrcap extension

#### C. No OutputFile/OutputDir (Sibling Path) (15 cases)
- ✅ Input="file.echoreplay" → "file.nevrcap" (same directory)
- ✅ Input="file.nevrcap" → "file.echoreplay"
- ✅ Input="/tmp/file.echoreplay" → "/tmp/file.nevrcap"
- ✅ Input="dir/file.echoreplay" → "dir/file.nevrcap"
- ✅ Input with no extension "file" → "file.nevrcap" or "file.echoreplay"
- ✅ Input with multiple dots "file.backup.echoreplay" → "file.backup.nevrcap"
- ✅ Input equals output (same format) → error or rename?
- ✅ Output file already exists, overwrite=false (error)
- ✅ Output file already exists, overwrite=true (allow)
- ✅ Input directory is read-only (can't write sibling)
- ✅ Input file with spaces "my file.echoreplay" → "my file.nevrcap"
- ✅ Input file with unicode "テスト.echoreplay" → "テスト.nevrcap"
- ✅ Input file in current directory "./file.echoreplay"
- ✅ Input file with symlink (resolve and use real path)
- ✅ Windows path normalization "C:\Users\file.echoreplay"

#### D. Extension Handling (5 cases)
- ✅ .echoreplay → .nevrcap conversion
- ✅ .nevrcap → .echoreplay conversion
- ✅ .ECHOREPLAY (uppercase) → .nevrcap
- ✅ .NevrCap (mixed case) → .echoreplay
- ✅ No extension → add appropriate extension based on format

---

### 4. `initProgressBar(totalFiles int, verbose bool) *progressbar.ProgressBar`
**Location**: Lines 684-697
**Complexity**: LOW (14 lines, 2 branches)
**Why High Priority**: User-facing feedback, affects UX significantly

**Function Signature**:
```go
func initProgressBar(totalFiles int, verbose bool) *progressbar.ProgressBar
```

**Key Logic**:
- Lines 686-688: Verbose mode (disable progress bar, return nil)
- Lines 690-697: Create progress bar with configuration

**Test Cases Needed** (~10 cases):

#### A. Progress Bar Creation (6 cases)
- ✅ verbose=false, totalFiles=1 (create bar)
- ✅ verbose=false, totalFiles=100 (create bar)
- ✅ verbose=false, totalFiles=10000 (create bar)
- ✅ verbose=true, totalFiles=1 (return nil)
- ✅ verbose=true, totalFiles=100 (return nil)
- ✅ totalFiles=0 (edge case - create bar or return nil?)

#### B. Progress Bar Configuration (4 cases)
- ✅ Progress bar max value equals totalFiles
- ✅ Progress bar description format (check string)
- ✅ Progress bar output to stderr (not stdout)
- ✅ Progress bar visual style (spinnerType, saucerHead, saucer, etc.)

---

### 5. `updateProgressBar(bar *progressbar.ProgressBar, success bool, successCount, failureCount *int) error`
**Location**: Lines 699-729
**Complexity**: LOW (31 lines, 3 branches)
**Why High Priority**: Accurate progress tracking, affects user feedback

**Function Signature**:
```go
func updateProgressBar(bar *progressbar.ProgressBar, success bool, successCount, failureCount *int) error
```

**Key Logic**:
- Lines 701-703: Bar is nil (no-op, return nil)
- Lines 705-710: Update success/failure counters
- Lines 712-727: Update bar description and increment

**Test Cases Needed** (~15 cases):

#### A. Nil Bar Handling (2 cases)
- ✅ bar=nil (return nil immediately, no panic)
- ✅ bar=nil with counter updates (counters updated, no panic)

#### B. Success Tracking (4 cases)
- ✅ success=true, increment successCount
- ✅ success=true, 10 times (successCount=10)
- ✅ success=true, failureCount unchanged
- ✅ Bar progress increments by 1

#### C. Failure Tracking (4 cases)
- ✅ success=false, increment failureCount
- ✅ success=false, 10 times (failureCount=10)
- ✅ success=false, successCount unchanged
- ✅ Bar progress increments by 1

#### D. Description Updates (5 cases)
- ✅ Description format includes successCount
- ✅ Description format includes failureCount
- ✅ Description updates dynamically (call multiple times)
- ✅ Description with 0 failures (failureCount=0)
- ✅ Description with 0 successes (successCount=0)

---

## Test Data Requirements

### A. Test Fixtures (Minimal for Tier 2)

These functions don't require extensive file fixtures (rely on Tier 1 fixtures):

```
testdata/converter/
├── comparison/                           # For compareJSONFrames tests
│   ├── identical_simple.json            # {a:1, b:"foo"}
│   ├── identical_nested.json            # {obj:{nested:{field:1}}}
│   ├── identical_array.json             # {arr:[1,2,3]}
│   ├── diff_value.json                  # One field value differs
│   ├── diff_field_added.json            # Extra field in actual
│   ├── diff_field_removed.json          # Missing field in actual
│   ├── diff_array_order.json            # Array elements reordered
│   ├── with_bones.json                  # Contains "BoneFrames" field
│   └── without_bones.json               # No "BoneFrames" field
```

### B. Mocking Requirements

**External Dependencies to Mock**:
1. **progressbar.ProgressBar**:
   - Create mock struct implementing progress bar interface
   - Track calls to `Add()`, `Describe()`, `Clear()`
   - Capture description strings for assertion

2. **File System** (for determineOutputFileForInput):
   - Use `os.CreateTemp()` for temporary directories
   - Use `filepath.Join()` for cross-platform paths
   - Mock file existence checks with `os.Stat()`

3. **Terminal Detection** (for progress bar tests):
   - Mock `os.Stdout.Fd()` for TTY detection
   - Set `TERM=dumb` environment variable to disable interactive features

### C. Helper Functions to Create

```go
// Helper: Create JSON frame map from struct
func createJSONFrame(t *testing.T, data map[string]interface{}) map[string]interface{} {
    // Deep copy to avoid mutation
    return data
}

// Helper: Create slice of N identical JSON frames
func createIdenticalFrames(t *testing.T, count int, data map[string]interface{}) []map[string]interface{} {
    frames := make([]map[string]interface{}, count)
    for i := 0; i < count; i++ {
        frames[i] = createJSONFrame(t, data)
    }
    return frames
}

// Helper: Modify a field in a JSON frame at specific path
func setJSONField(t *testing.T, frame map[string]interface{}, path string, value interface{}) {
    // path format: "field" or "nested.field" or "array[0].field"
    // Implementation needed
}

// Helper: Delete a field in a JSON frame at specific path
func deleteJSONField(t *testing.T, frame map[string]interface{}, path string) {
    // Implementation needed
}

// Helper: Mock progress bar for testing
type mockProgressBar struct {
    max           int
    current       int
    descriptions  []string
    addCalls      int
}

func (m *mockProgressBar) Add(n int) error {
    m.current += n
    m.addCalls++
    return nil
}

func (m *mockProgressBar) Describe(desc string) {
    m.descriptions = append(m.descriptions, desc)
}

// Helper: Create test config for determineOutputFileForInput tests
func createTestConverterConfig(t *testing.T, outputFile, outputDir, format string) *config.ConverterConfig {
    return &config.ConverterConfig{
        OutputFile: outputFile,
        OutputDir:  outputDir,
        Format:     format,
        Overwrite:  false,
    }
}
```

---

## Test File Structure

Create: `/home/andrew/src/nevr-agent/cmd/agent/converter_high_test.go`

```go
package agent

import (
    "testing"
    
    "github.com/nevrtech/nevr-agent/internal/config"
)

// Test compareJSONFrames - Frame Count
func TestCompareJSONFrames_FrameCount_Equal(t *testing.T) {
    // TODO: Implement
}

func TestCompareJSONFrames_FrameCount_ExpectedMore(t *testing.T) {
    // TODO: Implement
}

// Test compareJSONFrames - Identical Frames
func TestCompareJSONFrames_Identical_SingleFrame(t *testing.T) {
    // TODO: Implement
}

// Test compareJSONFrames - Frame Differences
func TestCompareJSONFrames_Difference_StringValue(t *testing.T) {
    // TODO: Implement
}

// Test compareJSONFrames - Bone Exclusion
func TestCompareJSONFrames_BoneExclusion_Enabled(t *testing.T) {
    // TODO: Implement
}

// Test compareNormalizedJSON - Key Comparison
func TestCompareNormalizedJSON_Keys_AllPresent(t *testing.T) {
    // TODO: Implement
}

func TestCompareNormalizedJSON_Keys_Missing(t *testing.T) {
    // TODO: Implement
}

// Test compareNormalizedJSON - Nil Handling
func TestCompareNormalizedJSON_Nil_BothNil(t *testing.T) {
    // TODO: Implement
}

// Test compareNormalizedJSON - Map Recursion
func TestCompareNormalizedJSON_MapRecursion_Nested(t *testing.T) {
    // TODO: Implement
}

// Test compareNormalizedJSON - Slice Comparison
func TestCompareNormalizedJSON_Slice_Identical(t *testing.T) {
    // TODO: Implement
}

// Test compareNormalizedJSON - Primitives
func TestCompareNormalizedJSON_Primitives_String(t *testing.T) {
    // TODO: Implement
}

// Test compareNormalizedJSON - Path Tracking
func TestCompareNormalizedJSON_Path_TopLevel(t *testing.T) {
    // TODO: Implement
}

// Test determineOutputFileForInput - Explicit OutputFile
func TestDetermineOutputFileForInput_ExplicitOutputFile(t *testing.T) {
    // TODO: Implement
}

// Test determineOutputFileForInput - OutputDir
func TestDetermineOutputFileForInput_OutputDir_BasicPath(t *testing.T) {
    // TODO: Implement
}

// Test determineOutputFileForInput - Sibling Path
func TestDetermineOutputFileForInput_SiblingPath_EchoReplayToNevrcap(t *testing.T) {
    // TODO: Implement
}

// Test determineOutputFileForInput - Extension Handling
func TestDetermineOutputFileForInput_Extension_Conversion(t *testing.T) {
    // TODO: Implement
}

// Test initProgressBar
func TestInitProgressBar_Verbose_Disabled(t *testing.T) {
    // TODO: Implement
}

func TestInitProgressBar_NonVerbose_Created(t *testing.T) {
    // TODO: Implement
}

// Test updateProgressBar
func TestUpdateProgressBar_NilBar(t *testing.T) {
    // TODO: Implement
}

func TestUpdateProgressBar_Success_IncrementCounter(t *testing.T) {
    // TODO: Implement
}

func TestUpdateProgressBar_Failure_IncrementCounter(t *testing.T) {
    // TODO: Implement
}

func TestUpdateProgressBar_Description_Updates(t *testing.T) {
    // TODO: Implement
}

// Helper functions
func createJSONFrame(t *testing.T, data map[string]interface{}) map[string]interface{} {
    // TODO: Implement
    return nil
}

func createIdenticalFrames(t *testing.T, count int, data map[string]interface{}) []map[string]interface{} {
    // TODO: Implement
    return nil
}

func setJSONField(t *testing.T, frame map[string]interface{}, path string, value interface{}) {
    // TODO: Implement
}

func deleteJSONField(t *testing.T, frame map[string]interface{}, path string) {
    // TODO: Implement
}

type mockProgressBar struct {
    max          int
    current      int
    descriptions []string
    addCalls     int
}

func (m *mockProgressBar) Add(n int) error {
    m.current += n
    m.addCalls++
    return nil
}

func (m *mockProgressBar) Describe(desc string) {
    m.descriptions = append(m.descriptions, desc)
}
```

---

## Acceptance Criteria

1. **Coverage Target**: 80%+ line coverage for all 5 functions
2. **Test Count**: ~160 test cases implemented (50 + 45 + 40 + 10 + 15)
3. **All Edge Cases Covered**: Including nil handling, path tracking, error messages
4. **Test Fixtures Created**: Minimal JSON fixtures in `testdata/converter/comparison/`
5. **Helper Functions Implemented**: All 6 helper functions created and documented
6. **Tests Pass**: `go test ./cmd/agent -v -run TestCompareJSONFrames|TestCompareNormalizedJSON|TestDetermineOutputFileForInput|TestInitProgressBar|TestUpdateProgressBar` exits 0
7. **No Testify**: All assertions use standard `testing` package
8. **Documentation**: Each test has clear comments explaining what it tests

---

## Notes for Implementation Agent

- **Prerequisite**: Tier 1 tests should be completed first (shared test fixtures)
- **JSON Comparison**: Focus on edge cases - these functions are the heart of validation
- **Path Tracking**: `compareNormalizedJSON` paths must be accurate for debugging
- **Error Messages**: User-facing, must be clear and actionable
- **Progress Bar**: Use mock struct, don't test actual terminal rendering
- **Cross-platform**: Path handling must work on Windows and Unix
- **Deep Equality**: Consider using `reflect.DeepEqual()` for complex JSON comparison tests

---

## Estimated Effort

- **Test Fixture Creation**: 1 hour (simple JSON files)
- **Helper Functions**: 2-3 hours (JSON manipulation helpers)
- **Test Implementation**: 5-7 hours (160 test cases, mostly assertion logic)
- **Debugging & Refinement**: 1-2 hours
- **Total**: 9-13 hours

**Priority**: HIGH - Complete after Tier 1, before Tier 3
