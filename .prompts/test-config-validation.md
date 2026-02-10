# Test Implementation: Config Validation (ValidateConverterConfig)

## Context

This prompt is for implementing comprehensive test coverage for `ValidateConverterConfig()` in `internal/config/config.go` and expanding its validation logic. The current implementation is **INCOMPLETE** and many critical validation rules are missing, leading to runtime errors that should be caught at configuration time.

**Current Coverage**: 20.5% (only ParseByteSize/FormatByteSize tested)
**Target Coverage**: 80%+ for config validation functions
**Test Framework**: Standard Go `testing` package (NO testify/assert library)

## Current State Analysis

### Existing Implementation (Lines 315-324)
```go
func ValidateConverterConfig(cfg *ConverterConfig) error {
    if cfg.InputFile == "" {
        return fmt.Errorf("input file is required")
    }
    
    if cfg.OutputFile == "" && cfg.OutputDir == "" {
        return fmt.Errorf("either output file or output directory is required")
    }
    
    return nil
}
```

### Critical Missing Validations (From Code Analysis)

**From converter.go lines 106-112** (validation that should be in config):
```go
if cfg.Validate && (cfg.Recursive || cfg.Glob != "") {
    return fmt.Errorf("--validate flag cannot be used with --recursive or --glob")
}
if (cfg.Recursive || cfg.Glob != "") && cfg.OutputDir == "" {
    return fmt.Errorf("--output-dir is required when using --recursive or --glob")
}
```

**Missing validations identified**:
1. ❌ `--validate` + `--recursive` combination check
2. ❌ `--validate` + `--glob` combination check
3. ❌ `--recursive` / `--glob` requires `--output-dir`
4. ❌ Format field validation ("auto", "echoreplay", "nevrcap" only)
5. ❌ InputFile must be file (not directory) when Recursive=false
6. ❌ InputFile must be directory when Recursive=true
7. ❌ Glob pattern syntax validation
8. ❌ OutputFile conflicts with Recursive/Glob (batch operations need OutputDir)
9. ❌ File existence checks (InputFile must exist)
10. ❌ Permission checks (InputFile readable, OutputDir writable)

### Environment Variable Support (Lines 164-229)

**Current `applyEnvOverrides()`** - Missing converter config support:
```go
func applyEnvOverrides(cfg *Config) {
    // ... stream config env vars ...
    // ... API config env vars ...
    // ❌ NO CONVERTER CONFIG ENV VARS
}
```

**Expected env vars** (based on other config sections):
- `EVR_CONVERTER_INPUT_FILE`
- `EVR_CONVERTER_OUTPUT_FILE`
- `EVR_CONVERTER_OUTPUT_DIR`
- `EVR_CONVERTER_FORMAT`
- `EVR_CONVERTER_VERBOSE`
- `EVR_CONVERTER_OVERWRITE`
- `EVR_CONVERTER_EXCLUDE_BONES`
- `EVR_CONVERTER_RECURSIVE`
- `EVR_CONVERTER_GLOB`
- `EVR_CONVERTER_VALIDATE`

---

## Validation Rules to Implement

### 1. Move CLI Validation to Config Layer

**REFACTOR REQUIRED**: Move validation logic from `converter.go` (lines 106-112) to `ValidateConverterConfig()`.

**Reason**: Configuration validation should happen at the config layer, not in command execution. This enables:
- Reusable validation across multiple command entry points
- Earlier error detection (fail fast)
- Testable validation logic without running full command
- Consistent error messages

### 2. Comprehensive Validation Rules

#### A. Required Fields (5 rules)
1. ✅ InputFile required (already implemented)
2. ✅ OutputFile or OutputDir required (already implemented)
3. ✅ Format must be "auto", "echoreplay", or "nevrcap" (NEW)
4. ✅ Recursive=true requires InputFile to be directory (NEW)
5. ✅ Recursive=false requires InputFile to be file (NEW)

#### B. Flag Combination Rules (10 rules)
1. ✅ `Validate=true` + `Recursive=true` → ERROR (NEW)
2. ✅ `Validate=true` + `Glob!=""` → ERROR (NEW)
3. ✅ `Recursive=true` + `OutputDir==""` → ERROR (NEW)
4. ✅ `Glob!=""` + `OutputDir==""` → ERROR (NEW)
5. ✅ `Recursive=true` + `OutputFile!=""` → ERROR (batch needs dir) (NEW)
6. ✅ `Glob!=""` + `OutputFile!=""` → ERROR (batch needs dir) (NEW)
7. ✅ `Recursive=true` + `Validate=true` → ERROR (duplicate of #1) (NEW)
8. ✅ `OutputFile!=""` + `OutputDir!=""` → ERROR (ambiguous) (NEW)
9. ✅ `InputFile` is directory + `Recursive=false` → ERROR (NEW)
10. ✅ `InputFile` is file + `Recursive=true` → ERROR (NEW)

#### C. File System Validation (8 rules)
1. ✅ InputFile must exist (NEW)
2. ✅ InputFile must be readable (NEW)
3. ✅ InputFile must be regular file (not symlink, not device) when Recursive=false (NEW)
4. ✅ InputFile must be directory when Recursive=true (NEW)
5. ✅ OutputDir must exist or be creatable (NEW)
6. ✅ OutputDir must be writable (NEW)
7. ✅ OutputFile parent directory must exist (NEW)
8. ✅ OutputFile must be writable (or not exist) (NEW)

#### D. Format Validation (6 rules)
1. ✅ Format must be "auto", "echoreplay", or "nevrcap" (NEW)
2. ✅ Format case-insensitive matching (accept "ECHOREPLAY", "Auto", etc.) (NEW)
3. ✅ Format="" defaults to "auto" (NEW)
4. ✅ Format="invalid" → ERROR (NEW)
5. ✅ Format with spaces "auto " → trimmed to "auto" (NEW)
6. ✅ Format with unicode characters → ERROR (NEW)

#### E. Glob Pattern Validation (6 rules)
1. ✅ Glob="" is valid (no filtering) (NEW)
2. ✅ Glob="*.echoreplay" is valid (NEW)
3. ✅ Glob="**/*.echoreplay" is valid (recursive pattern) (NEW)
4. ✅ Glob="[invalid" → ERROR (malformed pattern) (NEW)
5. ✅ Glob with invalid syntax → ERROR (NEW)
6. ✅ Glob with backslash on Windows → normalized (NEW)

---

## Functions to Implement/Expand

### 1. `ValidateConverterConfig(cfg *ConverterConfig) error`
**Location**: Lines 315-324 (EXPAND)
**Current Lines**: 10 lines
**Target Lines**: ~100 lines (10x expansion)

**Proposed Implementation Structure**:
```go
func ValidateConverterConfig(cfg *ConverterConfig) error {
    // 1. Required fields
    if err := validateRequiredFields(cfg); err != nil {
        return err
    }
    
    // 2. Format validation
    if err := validateFormat(cfg); err != nil {
        return err
    }
    
    // 3. Flag combination rules
    if err := validateFlagCombinations(cfg); err != nil {
        return err
    }
    
    // 4. File system validation
    if err := validateFileSystem(cfg); err != nil {
        return err
    }
    
    // 5. Glob pattern validation
    if err := validateGlobPattern(cfg); err != nil {
        return err
    }
    
    return nil
}

// Helper: Validate required fields
func validateRequiredFields(cfg *ConverterConfig) error {
    // Implementation
}

// Helper: Validate format field
func validateFormat(cfg *ConverterConfig) error {
    // Normalize to lowercase
    // Check against allowed values
}

// Helper: Validate flag combinations
func validateFlagCombinations(cfg *ConverterConfig) error {
    // Check all invalid combinations
}

// Helper: Validate file system paths
func validateFileSystem(cfg *ConverterConfig) error {
    // Check file existence, permissions, types
}

// Helper: Validate glob pattern syntax
func validateGlobPattern(cfg *ConverterConfig) error {
    // Use filepath.Match or doublestar for validation
}
```

**Test Cases Needed** (~100 cases):

#### A. Required Fields (10 cases)
- ✅ Valid config with all required fields
- ✅ InputFile missing (fail)
- ✅ InputFile empty string (fail)
- ✅ OutputFile and OutputDir both missing (fail)
- ✅ OutputFile provided, OutputDir empty (success)
- ✅ OutputDir provided, OutputFile empty (success)
- ✅ Both OutputFile and OutputDir provided (fail - ambiguous)
- ✅ InputFile with whitespace only (fail)
- ✅ OutputFile with whitespace only (fail)
- ✅ OutputDir with whitespace only (fail)

#### B. Format Validation (15 cases)
- ✅ Format="auto" (success)
- ✅ Format="echoreplay" (success)
- ✅ Format="nevrcap" (success)
- ✅ Format="" defaults to "auto" (success)
- ✅ Format="AUTO" (uppercase, normalized to "auto", success)
- ✅ Format="EchoReplay" (mixed case, normalized, success)
- ✅ Format="NEVRCAP" (uppercase, normalized, success)
- ✅ Format=" auto " (with spaces, trimmed, success)
- ✅ Format="invalid" (fail)
- ✅ Format="echoreplay2" (fail)
- ✅ Format="nevrcap_old" (fail)
- ✅ Format="echo-replay" (fail - hyphen not allowed)
- ✅ Format="nevr cap" (fail - space in middle)
- ✅ Format="テスト" (unicode, fail)
- ✅ Format with null byte (fail)

#### C. Flag Combinations (20 cases)
- ✅ Validate=true, Recursive=false, Glob="" (success)
- ✅ Validate=true, Recursive=true (fail)
- ✅ Validate=true, Glob="*.echoreplay" (fail)
- ✅ Validate=true, Recursive=true, Glob="*.echoreplay" (fail - both invalid)
- ✅ Recursive=true, OutputDir="/tmp" (success)
- ✅ Recursive=true, OutputDir="" (fail)
- ✅ Recursive=true, OutputFile="/tmp/out.nevrcap" (fail - needs OutputDir)
- ✅ Recursive=true, OutputFile="/tmp/out.nevrcap", OutputDir="/tmp" (fail - both set)
- ✅ Glob="*.echoreplay", OutputDir="/tmp" (success)
- ✅ Glob="*.echoreplay", OutputDir="" (fail)
- ✅ Glob="*.echoreplay", OutputFile="/tmp/out.nevrcap" (fail - needs OutputDir)
- ✅ Recursive=false, Glob="" (success - single file mode)
- ✅ Recursive=true, Glob="*.echoreplay" (success - both allowed together)
- ✅ Validate=false, Recursive=true (success - validation only blocks Validate=true)
- ✅ OutputFile and OutputDir both set (fail - ambiguous)
- ✅ InputFile is directory, Recursive=false (fail)
- ✅ InputFile is file, Recursive=true (fail)
- ✅ InputFile is symlink to file, Recursive=false (success)
- ✅ InputFile is symlink to directory, Recursive=true (success)
- ✅ All flags default/zero values (fail - InputFile required)

#### D. File System Validation (30 cases)
- ✅ InputFile exists, is regular file (success)
- ✅ InputFile doesn't exist (fail)
- ✅ InputFile is directory, Recursive=false (fail)
- ✅ InputFile is directory, Recursive=true (success)
- ✅ InputFile is symlink to file (success)
- ✅ InputFile is symlink to directory, Recursive=true (success)
- ✅ InputFile is symlink to non-existent file (fail)
- ✅ InputFile is device file /dev/null (fail)
- ✅ InputFile is named pipe (fail)
- ✅ InputFile is socket (fail)
- ✅ InputFile not readable (permission denied) (fail)
- ✅ InputFile readable but not regular file (fail)
- ✅ InputFile with absolute path (success)
- ✅ InputFile with relative path (success)
- ✅ InputFile with ~/ home directory (expanded, success)
- ✅ InputFile with ../ parent directory (normalized, success)
- ✅ OutputFile parent directory exists (success)
- ✅ OutputFile parent directory doesn't exist (fail)
- ✅ OutputFile parent directory not writable (fail)
- ✅ OutputFile already exists, Overwrite=false (fail)
- ✅ OutputFile already exists, Overwrite=true (success)
- ✅ OutputFile doesn't exist (success)
- ✅ OutputDir exists (success)
- ✅ OutputDir doesn't exist but parent exists (success - can create)
- ✅ OutputDir doesn't exist, parent doesn't exist (fail)
- ✅ OutputDir not writable (fail)
- ✅ OutputDir is file, not directory (fail)
- ✅ OutputDir with trailing slash (normalized, success)
- ✅ OutputDir with multiple trailing slashes (normalized, success)
- ✅ Paths with spaces "my file.echoreplay" (success)

#### E. Glob Pattern Validation (15 cases)
- ✅ Glob="" (success - no filtering)
- ✅ Glob="*.echoreplay" (success)
- ✅ Glob="**/*.echoreplay" (success - doublestar)
- ✅ Glob="test_*.nevrcap" (success)
- ✅ Glob="*_recording.echoreplay" (success)
- ✅ Glob="[0-9]*.echoreplay" (success - character class)
- ✅ Glob="[!test]*.echoreplay" (success - negation)
- ✅ Glob="{foo,bar}*.echoreplay" (success - brace expansion)
- ✅ Glob="[invalid" (fail - unclosed bracket)
- ✅ Glob="**/**/invalid" (fail - double doublestar)
- ✅ Glob with null byte (fail)
- ✅ Glob with backslash on Windows "test\\*.echoreplay" (normalized)
- ✅ Glob with unicode "テスト*.echoreplay" (success)
- ✅ Glob with spaces "test *.echoreplay" (success)
- ✅ Glob with absolute path "/tmp/*.echoreplay" (success)

#### F. Edge Cases (10 cases)
- ✅ Config with all fields nil/zero (fail - InputFile required)
- ✅ Config with all boolean flags true (various validation failures)
- ✅ Config with very long paths (>4096 chars) (platform-dependent)
- ✅ Config with unicode paths "テスト/ファイル.echoreplay" (success)
- ✅ Config with Windows paths on Unix (normalized)
- ✅ Config with Unix paths on Windows (normalized)
- ✅ Config with network paths "\\\\server\\share\\file.echoreplay" (Windows UNC)
- ✅ Config with relative paths ".././file.echoreplay" (normalized)
- ✅ Config with environment variables in paths "$HOME/file.echoreplay" (not expanded - should fail or handle explicitly)
- ✅ Config validated multiple times (idempotent, no side effects)

---

### 2. `applyEnvOverrides(cfg *Config) error`
**Location**: Lines 164-229 (EXPAND)
**Current**: No converter config support
**Target**: Add all converter config env var support

**Proposed Addition**:
```go
func applyEnvOverrides(cfg *Config) error {
    // ... existing stream/API config ...
    
    // Converter config overrides
    if v := os.Getenv("EVR_CONVERTER_INPUT_FILE"); v != "" {
        cfg.Converter.InputFile = v
    }
    if v := os.Getenv("EVR_CONVERTER_OUTPUT_FILE"); v != "" {
        cfg.Converter.OutputFile = v
    }
    if v := os.Getenv("EVR_CONVERTER_OUTPUT_DIR"); v != "" {
        cfg.Converter.OutputDir = v
    }
    if v := os.Getenv("EVR_CONVERTER_FORMAT"); v != "" {
        cfg.Converter.Format = v
    }
    if v := os.Getenv("EVR_CONVERTER_VERBOSE"); v != "" {
        cfg.Converter.Verbose = parseBool(v)
    }
    if v := os.Getenv("EVR_CONVERTER_OVERWRITE"); v != "" {
        cfg.Converter.Overwrite = parseBool(v)
    }
    if v := os.Getenv("EVR_CONVERTER_EXCLUDE_BONES"); v != "" {
        cfg.Converter.ExcludeBones = parseBool(v)
    }
    if v := os.Getenv("EVR_CONVERTER_RECURSIVE"); v != "" {
        cfg.Converter.Recursive = parseBool(v)
    }
    if v := os.Getenv("EVR_CONVERTER_GLOB"); v != "" {
        cfg.Converter.Glob = v
    }
    if v := os.Getenv("EVR_CONVERTER_VALIDATE"); v != "" {
        cfg.Converter.Validate = parseBool(v)
    }
    
    return nil
}

// Helper: Parse boolean from string (handle "true", "1", "yes", etc.)
func parseBool(s string) bool {
    s = strings.ToLower(strings.TrimSpace(s))
    return s == "true" || s == "1" || s == "yes" || s == "on"
}
```

**Test Cases Needed** (~25 cases):

#### A. String Fields (10 cases)
- ✅ EVR_CONVERTER_INPUT_FILE="/tmp/input.echoreplay" (override)
- ✅ EVR_CONVERTER_OUTPUT_FILE="/tmp/output.nevrcap" (override)
- ✅ EVR_CONVERTER_OUTPUT_DIR="/tmp/output" (override)
- ✅ EVR_CONVERTER_FORMAT="nevrcap" (override)
- ✅ EVR_CONVERTER_GLOB="*.echoreplay" (override)
- ✅ Env var set to empty string "" (should override to empty)
- ✅ Env var not set (no override, use config value)
- ✅ Multiple env vars set (all override)
- ✅ Env var with spaces " /tmp/file.echoreplay " (should trim)
- ✅ Env var with unicode "テスト.echoreplay" (override)

#### B. Boolean Fields (15 cases)
- ✅ EVR_CONVERTER_VERBOSE="true" (override to true)
- ✅ EVR_CONVERTER_VERBOSE="false" (override to false)
- ✅ EVR_CONVERTER_VERBOSE="1" (override to true)
- ✅ EVR_CONVERTER_VERBOSE="0" (override to false)
- ✅ EVR_CONVERTER_VERBOSE="yes" (override to true)
- ✅ EVR_CONVERTER_VERBOSE="no" (override to false)
- ✅ EVR_CONVERTER_VERBOSE="on" (override to true)
- ✅ EVR_CONVERTER_VERBOSE="off" (override to false)
- ✅ EVR_CONVERTER_VERBOSE="TRUE" (uppercase, override to true)
- ✅ EVR_CONVERTER_VERBOSE="Yes" (mixed case, override to true)
- ✅ EVR_CONVERTER_OVERWRITE="true" (override)
- ✅ EVR_CONVERTER_EXCLUDE_BONES="true" (override)
- ✅ EVR_CONVERTER_RECURSIVE="true" (override)
- ✅ EVR_CONVERTER_VALIDATE="true" (override)
- ✅ EVR_CONVERTER_VERBOSE="invalid" (defaults to false)

---

## Test File Structure

Create: `/home/andrew/src/nevr-agent/internal/config/converter_validation_test.go`

```go
package config

import (
    "os"
    "path/filepath"
    "testing"
)

// Test ValidateConverterConfig - Required Fields
func TestValidateConverterConfig_RequiredFields_AllPresent(t *testing.T) {
    // TODO: Implement
}

func TestValidateConverterConfig_RequiredFields_InputFileMissing(t *testing.T) {
    // TODO: Implement
}

// Test ValidateConverterConfig - Format Validation
func TestValidateConverterConfig_Format_Auto(t *testing.T) {
    // TODO: Implement
}

func TestValidateConverterConfig_Format_Invalid(t *testing.T) {
    // TODO: Implement
}

// Test ValidateConverterConfig - Flag Combinations
func TestValidateConverterConfig_FlagCombination_ValidateWithRecursive(t *testing.T) {
    // TODO: Implement
}

func TestValidateConverterConfig_FlagCombination_RecursiveRequiresOutputDir(t *testing.T) {
    // TODO: Implement
}

// Test ValidateConverterConfig - File System
func TestValidateConverterConfig_FileSystem_InputFileExists(t *testing.T) {
    // TODO: Implement
}

func TestValidateConverterConfig_FileSystem_InputFileNotExists(t *testing.T) {
    // TODO: Implement
}

// Test ValidateConverterConfig - Glob Pattern
func TestValidateConverterConfig_Glob_ValidPattern(t *testing.T) {
    // TODO: Implement
}

func TestValidateConverterConfig_Glob_InvalidPattern(t *testing.T) {
    // TODO: Implement
}

// Test applyEnvOverrides - String Fields
func TestApplyEnvOverrides_ConverterInputFile(t *testing.T) {
    // TODO: Implement
}

// Test applyEnvOverrides - Boolean Fields
func TestApplyEnvOverrides_ConverterVerboseTrue(t *testing.T) {
    // TODO: Implement
}

func TestApplyEnvOverrides_ConverterVerboseFalse(t *testing.T) {
    // TODO: Implement
}

// Helper functions
func createTempFile(t *testing.T, name string) string {
    dir := t.TempDir()
    path := filepath.Join(dir, name)
    f, err := os.Create(path)
    if err != nil {
        t.Fatalf("createTempFile: %v", err)
    }
    f.Close()
    return path
}

func createTempDir(t *testing.T, name string) string {
    dir := t.TempDir()
    path := filepath.Join(dir, name)
    if err := os.Mkdir(path, 0755); err != nil {
        t.Fatalf("createTempDir: %v", err)
    }
    return path
}

func setEnv(t *testing.T, key, value string) {
    old := os.Getenv(key)
    os.Setenv(key, value)
    t.Cleanup(func() {
        if old == "" {
            os.Unsetenv(key)
        } else {
            os.Setenv(key, old)
        }
    })
}
```

---

## Refactoring Required in converter.go

**MOVE** validation logic from `converter.go` lines 106-112 to `config.ValidateConverterConfig()`.

**Before** (converter.go):
```go
func runConverter(ctx context.Context, cmd *cobra.Command, cfg *config.ConverterConfig) error {
    // Validation here (WRONG PLACE)
    if cfg.Validate && (cfg.Recursive || cfg.Glob != "") {
        return fmt.Errorf("--validate flag cannot be used with --recursive or --glob")
    }
    if (cfg.Recursive || cfg.Glob != "") && cfg.OutputDir == "" {
        return fmt.Errorf("--output-dir is required when using --recursive or --glob")
    }
    // ... rest of function
}
```

**After** (converter.go):
```go
func runConverter(ctx context.Context, cmd *cobra.Command, cfg *config.ConverterConfig) error {
    // Validation moved to config layer - just call it here
    if err := config.ValidateConverterConfig(cfg); err != nil {
        return err
    }
    // ... rest of function
}
```

**After** (config.go):
```go
func ValidateConverterConfig(cfg *ConverterConfig) error {
    // All validation logic here
    if cfg.Validate && (cfg.Recursive || cfg.Glob != "") {
        return fmt.Errorf("--validate flag cannot be used with --recursive or --glob")
    }
    if (cfg.Recursive || cfg.Glob != "") && cfg.OutputDir == "" {
        return fmt.Errorf("--output-dir is required when using --recursive or --glob")
    }
    // ... all other validation
}
```

---

## Acceptance Criteria

1. **Validation Logic Moved**: Lines 106-112 from converter.go moved to config.go
2. **Coverage Target**: 80%+ line coverage for `ValidateConverterConfig()` and related helpers
3. **Test Count**: ~150 test cases implemented (100 + 25 + 25 for refactoring verification)
4. **All Validation Rules Implemented**: 35 validation rules (5 required + 10 combinations + 8 filesystem + 6 format + 6 glob)
5. **Environment Variable Support**: All 10 converter config env vars working
6. **Helper Functions**: 5 validation helper functions created (`validateRequiredFields`, etc.)
7. **Tests Pass**: `go test ./internal/config -v -run TestValidateConverterConfig|TestApplyEnvOverrides` exits 0
8. **No Regression**: Existing config tests still pass after changes
9. **Documentation**: Each validation rule has clear error message
10. **Refactoring Complete**: converter.go validation removed, calls config.ValidateConverterConfig() instead

---

## Notes for Implementation Agent

- **Refactoring First**: Move validation from converter.go to config.go BEFORE adding new validation
- **Test Existing Behavior**: Ensure moved validation behaves identically
- **Add Tests for New Rules**: After refactoring, add new validation rules with tests
- **Environment Variables**: Add env var support AFTER validation is complete
- **Error Messages**: Must be clear, actionable, and consistent
- **Platform Compatibility**: Path validation must work on Windows and Unix
- **No Breaking Changes**: Existing valid configs must remain valid

---

## Estimated Effort

- **Validation Refactoring**: 2-3 hours (move + verify no regression)
- **New Validation Rules**: 3-4 hours (35 rules + helpers)
- **Environment Variable Support**: 1-2 hours
- **Test Implementation**: 6-8 hours (150 test cases)
- **Debugging & Refinement**: 2-3 hours
- **Total**: 14-20 hours

**Priority**: CRITICAL - Foundational for reliable converter operation
