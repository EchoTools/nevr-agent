package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ========================================
// Test ValidateConverterConfig - Required Fields
// ========================================

func TestValidateConverterConfig_RequiredFields_AllPresent(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Format:    "auto",
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() with all required fields failed: %v", err)
	}
}

func TestValidateConverterConfig_RequiredFields_InputFileMissing(t *testing.T) {
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: "",
			OutputDir: tmpDir,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when InputFile is missing")
	}
}

func TestValidateConverterConfig_RequiredFields_InputFileEmpty(t *testing.T) {
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: "   ",
			OutputDir: tmpDir,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when InputFile is whitespace only")
	}
}

func TestValidateConverterConfig_RequiredFields_OutputMissing(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile:  tmpFile,
			OutputFile: "",
			OutputDir:  "",
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when both OutputFile and OutputDir are missing")
	}
}

func TestValidateConverterConfig_RequiredFields_OutputFileOnly(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.nevrcap")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile:  tmpFile,
			OutputFile: outputFile,
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() with OutputFile only should succeed: %v", err)
	}
}

func TestValidateConverterConfig_RequiredFields_OutputDirOnly(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() with OutputDir only should succeed: %v", err)
	}
}

func TestValidateConverterConfig_RequiredFields_BothOutputs(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	outputFile := filepath.Join(tmpDir, "output.nevrcap")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile:  tmpFile,
			OutputFile: outputFile,
			OutputDir:  tmpDir,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when both OutputFile and OutputDir are specified")
	}
}

// ========================================
// Test ValidateConverterConfig - Format Validation
// ========================================

func TestValidateConverterConfig_Format_Auto(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Format:    "auto",
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() with format='auto' failed: %v", err)
	}
	if cfg.Converter.Format != "auto" {
		t.Errorf("Format should be 'auto', got %q", cfg.Converter.Format)
	}
}

func TestValidateConverterConfig_Format_EchoReplay(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Format:    "echoreplay",
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() with format='echoreplay' failed: %v", err)
	}
}

func TestValidateConverterConfig_Format_Nevrcap(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Format:    "nevrcap",
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() with format='nevrcap' failed: %v", err)
	}
}

func TestValidateConverterConfig_Format_Empty(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Format:    "",
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() with empty format failed: %v", err)
	}
	// Should default to "auto"
	if cfg.Converter.Format != "auto" {
		t.Errorf("Empty format should default to 'auto', got %q", cfg.Converter.Format)
	}
}

func TestValidateConverterConfig_Format_Uppercase(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Format:    "AUTO",
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() with uppercase format failed: %v", err)
	}
	// Should normalize to lowercase
	if cfg.Converter.Format != "auto" {
		t.Errorf("Format should be normalized to 'auto', got %q", cfg.Converter.Format)
	}
}

func TestValidateConverterConfig_Format_MixedCase(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Format:    "EchoReplay",
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() with mixed case format failed: %v", err)
	}
	// Should normalize to lowercase
	if cfg.Converter.Format != "echoreplay" {
		t.Errorf("Format should be normalized to 'echoreplay', got %q", cfg.Converter.Format)
	}
}

func TestValidateConverterConfig_Format_WithSpaces(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Format:    " auto ",
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() with spaced format failed: %v", err)
	}
	// Should trim spaces
	if cfg.Converter.Format != "auto" {
		t.Errorf("Format should be trimmed to 'auto', got %q", cfg.Converter.Format)
	}
}

func TestValidateConverterConfig_Format_Invalid(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Format:    "invalid",
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail with invalid format")
	}
}

// ========================================
// Test ValidateConverterConfig - Flag Combinations
// ========================================

func TestValidateConverterConfig_FlagCombination_ValidateWithRecursive(t *testing.T) {
	tmpDir := createTempDir(t, "input")
	outputDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpDir,
			OutputDir: outputDir,
			Validate:  true,
			Recursive: true,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when Validate and Recursive are both true")
	}
}

func TestValidateConverterConfig_FlagCombination_ValidateWithGlob(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Validate:  true,
			Glob:      "*.echoreplay",
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when Validate and Glob are both set")
	}
}

func TestValidateConverterConfig_FlagCombination_ValidateWithExcludeBones(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.nevrcap")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile:    tmpFile,
			OutputFile:   outputFile,
			Validate:     true,
			ExcludeBones: true,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when Validate and ExcludeBones are both true")
	}
}

func TestValidateConverterConfig_FlagCombination_RecursiveRequiresOutputDir(t *testing.T) {
	tmpDir := createTempDir(t, "input")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpDir,
			OutputDir: "",
			Recursive: true,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when Recursive is true but OutputDir is not set")
	}
}

func TestValidateConverterConfig_FlagCombination_GlobRequiresOutputDir(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: "",
			Glob:      "*.echoreplay",
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when Glob is set but OutputDir is not")
	}
}

func TestValidateConverterConfig_FlagCombination_RecursiveWithOutputFile(t *testing.T) {
	tmpDir := createTempDir(t, "input")
	tmpOutput := filepath.Join(t.TempDir(), "output.nevrcap")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile:  tmpDir,
			OutputFile: tmpOutput,
			Recursive:  true,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when Recursive and OutputFile are both set")
	}
}

func TestValidateConverterConfig_FlagCombination_GlobWithOutputFile(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpOutput := filepath.Join(t.TempDir(), "output.nevrcap")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile:  tmpFile,
			OutputFile: tmpOutput,
			Glob:       "*.echoreplay",
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when Glob and OutputFile are both set")
	}
}

// ========================================
// Test ValidateConverterConfig - File System
// ========================================

func TestValidateConverterConfig_FileSystem_InputFileExists(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() should succeed with existing input file: %v", err)
	}
}

func TestValidateConverterConfig_FileSystem_InputFileNotExists(t *testing.T) {
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: "/nonexistent/file.echoreplay",
			OutputDir: tmpDir,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when input file doesn't exist")
	}
}

func TestValidateConverterConfig_FileSystem_InputIsDirectory(t *testing.T) {
	tmpDir := createTempDir(t, "input")
	outputDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpDir,
			OutputDir: outputDir,
			Recursive: false,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when input is directory but Recursive is false")
	}
}

func TestValidateConverterConfig_FileSystem_RecursiveInputIsFile(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	outputDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: outputDir,
			Recursive: true,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when Recursive is true but input is a file")
	}
}

func TestValidateConverterConfig_FileSystem_RecursiveInputIsDirectory(t *testing.T) {
	tmpDir := createTempDir(t, "input")
	outputDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpDir,
			OutputDir: outputDir,
			Recursive: true,
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() should succeed when Recursive is true and input is directory: %v", err)
	}
}

func TestValidateConverterConfig_FileSystem_OutputDirExists(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() should succeed with existing output directory: %v", err)
	}
}

func TestValidateConverterConfig_FileSystem_OutputDirNotExists(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	parentDir := t.TempDir()
	nonExistentDir := filepath.Join(parentDir, "newdir")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: nonExistentDir,
		},
	}
	
	// Should succeed - directory can be created
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() should succeed when output directory can be created: %v", err)
	}
}

func TestValidateConverterConfig_FileSystem_OutputFileParentExists(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.nevrcap")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile:  tmpFile,
			OutputFile: outputFile,
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() should succeed with valid output file path: %v", err)
	}
}

func TestValidateConverterConfig_FileSystem_OutputFileParentNotExists(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	outputFile := "/nonexistent/dir/output.nevrcap"
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile:  tmpFile,
			OutputFile: outputFile,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when output file parent directory doesn't exist")
	}
}

func TestValidateConverterConfig_FileSystem_OutputFileExistsNoOverwrite(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.nevrcap")
	// Create the output file
	if f, err := os.Create(outputFile); err == nil {
		f.Close()
	}
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile:  tmpFile,
			OutputFile: outputFile,
			Overwrite:  false,
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail when output file exists and Overwrite is false")
	}
}

func TestValidateConverterConfig_FileSystem_OutputFileExistsWithOverwrite(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.nevrcap")
	// Create the output file
	if f, err := os.Create(outputFile); err == nil {
		f.Close()
	}
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile:  tmpFile,
			OutputFile: outputFile,
			Overwrite:  true,
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() should succeed when output file exists and Overwrite is true: %v", err)
	}
}

// ========================================
// Test ValidateConverterConfig - Glob Pattern
// ========================================

func TestValidateConverterConfig_Glob_Empty(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Glob:      "",
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() should succeed with empty glob: %v", err)
	}
}

func TestValidateConverterConfig_Glob_ValidPattern(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Glob:      "*.echoreplay",
		},
	}
	
	if err := cfg.ValidateConverterConfig(); err != nil {
		t.Errorf("ValidateConverterConfig() should succeed with valid glob pattern: %v", err)
	}
}

func TestValidateConverterConfig_Glob_InvalidPattern(t *testing.T) {
	tmpFile := createTempFile(t, "test.echoreplay")
	tmpDir := createTempDir(t, "output")
	
	cfg := &Config{
		Converter: ConverterConfig{
			InputFile: tmpFile,
			OutputDir: tmpDir,
			Glob:      "[invalid",
		},
	}
	
	err := cfg.ValidateConverterConfig()
	if err == nil {
		t.Error("ValidateConverterConfig() should fail with invalid glob pattern")
	}
}

// ========================================
// Test applyEnvOverrides - Converter String Fields
// ========================================

func TestApplyEnvOverrides_ConverterInputFile(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_INPUT_FILE", "/tmp/test.echoreplay")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if cfg.Converter.InputFile != "/tmp/test.echoreplay" {
		t.Errorf("InputFile = %q, want %q", cfg.Converter.InputFile, "/tmp/test.echoreplay")
	}
}

func TestApplyEnvOverrides_ConverterOutputFile(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_OUTPUT_FILE", "/tmp/output.nevrcap")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if cfg.Converter.OutputFile != "/tmp/output.nevrcap" {
		t.Errorf("OutputFile = %q, want %q", cfg.Converter.OutputFile, "/tmp/output.nevrcap")
	}
}

func TestApplyEnvOverrides_ConverterOutputDir(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_OUTPUT_DIR", "/tmp/output")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if cfg.Converter.OutputDir != "/tmp/output" {
		t.Errorf("OutputDir = %q, want %q", cfg.Converter.OutputDir, "/tmp/output")
	}
}

func TestApplyEnvOverrides_ConverterFormat(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_FORMAT", "nevrcap")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if cfg.Converter.Format != "nevrcap" {
		t.Errorf("Format = %q, want %q", cfg.Converter.Format, "nevrcap")
	}
}

func TestApplyEnvOverrides_ConverterGlob(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_GLOB", "*.echoreplay")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if cfg.Converter.Glob != "*.echoreplay" {
		t.Errorf("Glob = %q, want %q", cfg.Converter.Glob, "*.echoreplay")
	}
}

// ========================================
// Test applyEnvOverrides - Converter Boolean Fields
// ========================================

func TestApplyEnvOverrides_ConverterVerboseTrue(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_VERBOSE", "true")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if !cfg.Converter.Verbose {
		t.Error("Verbose should be true")
	}
}

func TestApplyEnvOverrides_ConverterVerboseFalse(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_VERBOSE", "false")
	
	cfg := DefaultConfig()
	cfg.Converter.Verbose = true // Set to true initially
	applyEnvOverrides(cfg)
	
	if cfg.Converter.Verbose {
		t.Error("Verbose should be false")
	}
}

func TestApplyEnvOverrides_ConverterVerboseOne(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_VERBOSE", "1")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if !cfg.Converter.Verbose {
		t.Error("Verbose should be true with '1'")
	}
}

func TestApplyEnvOverrides_ConverterVerboseYes(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_VERBOSE", "yes")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if !cfg.Converter.Verbose {
		t.Error("Verbose should be true with 'yes'")
	}
}

func TestApplyEnvOverrides_ConverterVerboseOn(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_VERBOSE", "on")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if !cfg.Converter.Verbose {
		t.Error("Verbose should be true with 'on'")
	}
}

func TestApplyEnvOverrides_ConverterOverwrite(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_OVERWRITE", "true")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if !cfg.Converter.Overwrite {
		t.Error("Overwrite should be true")
	}
}

func TestApplyEnvOverrides_ConverterExcludeBones(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_EXCLUDE_BONES", "true")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if !cfg.Converter.ExcludeBones {
		t.Error("ExcludeBones should be true")
	}
}

func TestApplyEnvOverrides_ConverterRecursive(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_RECURSIVE", "true")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if !cfg.Converter.Recursive {
		t.Error("Recursive should be true")
	}
}

func TestApplyEnvOverrides_ConverterValidate(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_VALIDATE", "true")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if !cfg.Converter.Validate {
		t.Error("Validate should be true")
	}
}

func TestApplyEnvOverrides_ConverterBooleanInvalid(t *testing.T) {
	setEnv(t, "EVR_CONVERTER_VERBOSE", "invalid")
	
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	
	if cfg.Converter.Verbose {
		t.Error("Verbose should be false with invalid value")
	}
}

// ========================================
// Helper functions
// ========================================

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
