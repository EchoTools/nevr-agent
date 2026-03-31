package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/echotools/nevr-capture/v3/pkg/codecs"
	"github.com/echotools/nevr-capture/v3/pkg/conversion"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	convInputFile    string
	convOutputFile   string
	convOutputDir    string
	convFormat       string
	convVerbose      bool
	convOverwrite    bool
	convShowProgress bool
	convExcludeBones bool
	convRecursive    bool
	convGlob         string
	convValidate     bool
)

func newConverterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Convert between .echoreplay and .nevrcap file formats",
		Long: `The convert command converts replay files between the .echoreplay 
(zip format) and .nevrcap (zstd compressed) formats.`,
		Example: `  # Convert echoreplay to nevrcap
	  agent convert --input game.echoreplay

  # Convert nevrcap to echoreplay
	  agent convert --input game.nevrcap

  # Force specific output format
	  agent convert --input game.echoreplay --format nevrcap

  # Specify output file
	  agent convert --input game.nevrcap --output converted.echoreplay
	  
  # Show progress bar during conversion
	  agent convert --input game.echoreplay --progress
	  
  # Exclude player bone data from output
	  agent convert --input game.echoreplay --exclude-bones

  # Convert all files in a directory recursively
	  agent convert --input ./recordings --recursive

  # Convert files matching a glob pattern
	  agent convert --input ./recordings --glob "*.echoreplay"

  # Combine recursive and glob
	  agent convert --input ./recordings --recursive --glob "rec_*.echoreplay"

  # Validate data integrity via round-trip conversion
	  agent convert --input game.echoreplay --validate`,
		RunE: runConverter,
	}

	// Converter-specific flags
	cmd.Flags().StringVarP(&convInputFile, "input", "i", "", "Input file path (.echoreplay or .nevrcap) (required)")
	cmd.Flags().StringVarP(&convOutputFile, "output", "o", "", "Output file path (optional, format detected from extension)")
	cmd.Flags().StringVar(&convOutputDir, "output-dir", "./", "Output directory for converted files")
	cmd.Flags().StringVarP(&convFormat, "format", "f", "auto", "Output format: auto, echoreplay, nevrcap")
	cmd.Flags().BoolVarP(&convVerbose, "verbose", "v", false, "Enable verbose logging")
	cmd.Flags().BoolVar(&convOverwrite, "overwrite", false, "Overwrite existing output files")
	cmd.Flags().BoolVarP(&convShowProgress, "progress", "p", false, "Show progress bar during conversion")
	cmd.Flags().BoolVar(&convExcludeBones, "exclude-bones", false, "Exclude player bone data from frames")
	cmd.Flags().BoolVarP(&convRecursive, "recursive", "r", false, "Recursively search directories for files to convert")
	cmd.Flags().StringVarP(&convGlob, "glob", "g", "", "Glob pattern to match files (e.g., '*.echoreplay')")
	cmd.Flags().BoolVar(&convValidate, "validate", false, "Validate data integrity via round-trip conversion (echoreplay only)")

	cmd.MarkFlagRequired("input")

	return cmd
}

func runConverter(cmd *cobra.Command, args []string) error {
	// Use flag values directly
	cfg.Converter.InputFile = convInputFile
	cfg.Converter.OutputFile = convOutputFile
	cfg.Converter.OutputDir = convOutputDir
	cfg.Converter.Format = convFormat
	cfg.Converter.Verbose = convVerbose
	cfg.Converter.Overwrite = convOverwrite
	cfg.Converter.ExcludeBones = convExcludeBones
	cfg.Converter.Recursive = convRecursive
	cfg.Converter.Glob = convGlob
	cfg.Converter.Validate = convValidate

	// Validate configuration (all validation moved to config layer)
	if err := cfg.ValidateConverterConfig(); err != nil {
		return err
	}

	// Discover files to convert
	files, err := discoverFiles()
	if err != nil {
		return fmt.Errorf("failed to discover files: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no files found to convert")
	}

	if cfg.Converter.Verbose {
		logger.Info("Found files to convert",
			zap.Int("count", len(files)))
	}

	// Convert all discovered files
	successCount := 0
	failCount := 0
	startTime := time.Now()

	for i, inputFile := range files {
		if cfg.Converter.Verbose {
			logger.Info("Converting file",
				zap.Int("progress", i+1),
				zap.Int("total", len(files)),
				zap.String("file", inputFile))
		} else if len(files) > 1 {
			fmt.Printf("Converting %d/%d: %s\n", i+1, len(files), filepath.Base(inputFile))
		}

		// Determine output file for this input
		outputFile, err := determineOutputFileForInput(inputFile)
		if err != nil {
			logger.Error("Failed to determine output file",
				zap.String("input", inputFile),
				zap.Error(err))
			failCount++
			continue
		}

		// Check if output file exists
		if _, err := os.Stat(outputFile); err == nil && !cfg.Converter.Overwrite {
			if cfg.Converter.Verbose {
				logger.Info("Skipping existing file (use --overwrite to overwrite)",
					zap.String("output", outputFile))
			}
			continue
		}

		// Perform conversion
		stats, err := convertFile(inputFile, outputFile, convShowProgress && len(files) == 1)
		if err != nil {
			logger.Error("Conversion failed",
				zap.String("input", inputFile),
				zap.Error(err))
			failCount++
			continue
		}

		successCount++

		if cfg.Converter.Validate {
			if err := validateRoundTrip(inputFile); err != nil {
				logger.Error("Validation failed",
					zap.String("input", inputFile),
					zap.Error(err))
				failCount++
				successCount--
				continue
			}
			logger.Info("Validation passed", zap.String("input", inputFile))
		}

		if cfg.Converter.Verbose || len(files) == 1 {
			logger.Info("Conversion completed",
				zap.String("output", outputFile),
				zap.Int("frames", stats.FrameCount),
				zap.Int64("input_size", stats.InputSize),
				zap.Int64("output_size", stats.OutputSize))

			if stats.InputSize > 0 {
				compressionRatio := float64(stats.OutputSize) / float64(stats.InputSize) * 100
				logger.Info("Compression ratio", zap.Float64("ratio", compressionRatio))
			}
		}
	}

	// Report summary
	duration := time.Since(startTime)
	logger.Info("Batch conversion completed",
		zap.Int("successful", successCount),
		zap.Int("failed", failCount),
		zap.Int("total", len(files)),
		zap.Duration("duration", duration))

	if failCount > 0 {
		return fmt.Errorf("conversion completed with %d failures", failCount)
	}

	return nil
}

type ConversionStats struct {
	FrameCount int
	InputSize  int64
	OutputSize int64
}

func convertFile(inputFile, outputFile string, showProgress bool) (*ConversionStats, error) {
	stats := &ConversionStats{}

	// Get input file size
	if inputInfo, err := os.Stat(inputFile); err == nil {
		stats.InputSize = inputInfo.Size()
	}

	// Determine input and output formats
	inputFormat := getFileFormat(inputFile)
	outputFormat := getFileFormat(outputFile)

	if cfg.Converter.Verbose {
		logger.Info("Converting",
			zap.String("from", inputFormat),
			zap.String("to", outputFormat))
	}

	// Perform conversion with progress support
	if inputFormat == "echoreplay" && outputFormat == "nevrcap" {
		if showProgress || cfg.Converter.ExcludeBones {
			if err := convertEchoReplayToNevrcapWithProgress(inputFile, outputFile); err != nil {
				return nil, err
			}
		} else {
			if err := conversion.ConvertEchoReplayToNevrcap(inputFile, outputFile); err != nil {
				return nil, err
			}
		}
	} else if inputFormat == "nevrcap" && outputFormat == "echoreplay" {
		if showProgress || cfg.Converter.ExcludeBones {
			if err := convertNevrcapToEchoReplayWithProgress(inputFile, outputFile); err != nil {
				return nil, err
			}
		} else {
			if err := conversion.ConvertNevrcapToEchoReplay(inputFile, outputFile); err != nil {
				return nil, err
			}
		}
	} else if inputFormat == outputFormat {
		// Same format, just copy (or re-write if excluding bones)
		if cfg.Converter.ExcludeBones {
			return convertSameFormat(inputFile, outputFile, inputFormat)
		}
		return copyFile(inputFile, outputFile)
	} else {
		return nil, fmt.Errorf("unsupported conversion from %s to %s", inputFormat, outputFormat)
	}

	// Get output file size
	if outputInfo, err := os.Stat(outputFile); err == nil {
		stats.OutputSize = outputInfo.Size()
	}

	// Count frames
	if frameCount, err := countFrames(outputFile); err == nil {
		stats.FrameCount = frameCount
	}

	return stats, nil
}

// convertEchoReplayToNevrcapWithProgress converts with optional progress bar
func convertEchoReplayToNevrcapWithProgress(inputFile, outputFile string) error {
	reader, err := codecs.NewEchoReplayReader(inputFile)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer reader.Close()

	writer, err := codecs.NewNevrCapWriter(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer writer.Close()

	// Count total frames first for progress bar (only if showing progress)
	totalFrames := 0
	var bar *progressbar.ProgressBar
	if convShowProgress {
		countReader, err := codecs.NewEchoReplayReader(inputFile)
		if err == nil {
			for {
				if _, err := countReader.ReadFrame(); err != nil {
					break
				}
				totalFrames++
			}
			countReader.Close()
		}

		bar = progressbar.NewOptions(totalFrames,
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionShowBytes(false),
			progressbar.OptionSetWidth(40),
			progressbar.OptionSetDescription("[cyan]Converting[reset]"),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "[green]=[reset]",
				SaucerHead:    "[green]>[reset]",
				SaucerPadding: " ",
				BarStart:      "[",
				BarEnd:        "]",
			}),
			progressbar.OptionShowCount(),
			progressbar.OptionShowElapsedTimeOnFinish(),
		)
	}

	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		// Exclude bones if configured
		if cfg.Converter.ExcludeBones {
			frame.PlayerBones = nil
		}

		if err := writer.WriteFrame(frame); err != nil {
			return fmt.Errorf("failed to write frame: %w", err)
		}

		if bar != nil {
			bar.Add(1)
		}
	}

	if bar != nil {
		fmt.Println() // New line after progress bar
	}
	return nil
}

// convertNevrcapToEchoReplayWithProgress converts with optional progress bar
func convertNevrcapToEchoReplayWithProgress(inputFile, outputFile string) error {
	reader, err := codecs.NewNevrCapReader(inputFile)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer reader.Close()

	// Skip header
	if _, err := reader.ReadHeader(); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	writer, err := codecs.NewEchoReplayWriter(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer writer.Close()

	// Count total frames first for progress bar (only if showing progress)
	totalFrames := 0
	var bar *progressbar.ProgressBar
	if convShowProgress {
		countReader, err := codecs.NewNevrCapReader(inputFile)
		if err == nil {
			if _, err := countReader.ReadHeader(); err == nil {
				for {
					if _, err := countReader.ReadFrame(); err != nil {
						break
					}
					totalFrames++
				}
			}
			countReader.Close()
		}

		bar = progressbar.NewOptions(totalFrames,
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionShowBytes(false),
			progressbar.OptionSetWidth(40),
			progressbar.OptionSetDescription("[cyan]Converting[reset]"),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "[green]=[reset]",
				SaucerHead:    "[green]>[reset]",
				SaucerPadding: " ",
				BarStart:      "[",
				BarEnd:        "]",
			}),
			progressbar.OptionShowCount(),
			progressbar.OptionShowElapsedTimeOnFinish(),
		)
	}

	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		// Exclude bones if configured
		if cfg.Converter.ExcludeBones {
			frame.PlayerBones = nil
		}

		if err := writer.WriteFrame(frame); err != nil {
			return fmt.Errorf("failed to write frame: %w", err)
		}

		if bar != nil {
			bar.Add(1)
		}
	}

	if bar != nil {
		fmt.Println() // New line after progress bar
	}
	return nil
}

func getFileFormat(filename string) string {
	lowerFile := strings.ToLower(filename)
	if strings.HasSuffix(lowerFile, ".echoreplay") {
		return "echoreplay"
	} else if strings.HasSuffix(lowerFile, ".nevrcap") {
		return "nevrcap"
	}
	return "unknown"
}

func copyFile(src, dst string) (*ConversionStats, error) {
	stats := &ConversionStats{}

	input, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer input.Close()

	output, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	defer output.Close()

	written, err := io.Copy(output, input)
	if err != nil {
		return nil, err
	}

	stats.InputSize = written
	stats.OutputSize = written

	if frameCount, err := countFrames(dst); err == nil {
		stats.FrameCount = frameCount
	}

	return stats, nil
}

func convertSameFormat(inputFile, outputFile, format string) (*ConversionStats, error) {
	stats := &ConversionStats{}

	// Get input file size
	if inputInfo, err := os.Stat(inputFile); err == nil {
		stats.InputSize = inputInfo.Size()
	}

	switch format {
	case "echoreplay":
		reader, err := codecs.NewEchoReplayReader(inputFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open input file: %w", err)
		}
		defer reader.Close()

		writer, err := codecs.NewEchoReplayWriter(outputFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create output file: %w", err)
		}
		defer writer.Close()

		for {
			frame, err := reader.ReadFrame()
			if err != nil {
				if err == io.EOF {
					break
				}
				return nil, fmt.Errorf("failed to read frame: %w", err)
			}

			// Exclude bones if configured
			if cfg.Converter.ExcludeBones {
				frame.PlayerBones = nil
			}

			if err := writer.WriteFrame(frame); err != nil {
				return nil, fmt.Errorf("failed to write frame: %w", err)
			}
			stats.FrameCount++
		}

	case "nevrcap":
		reader, err := codecs.NewNevrCapReader(inputFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open input file: %w", err)
		}
		defer reader.Close()

		// Read header
		if _, err := reader.ReadHeader(); err != nil {
			return nil, fmt.Errorf("failed to read header: %w", err)
		}

		writer, err := codecs.NewNevrCapWriter(outputFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create output file: %w", err)
		}
		defer writer.Close()

		for {
			frame, err := reader.ReadFrame()
			if err != nil {
				if err == io.EOF {
					break
				}
				return nil, fmt.Errorf("failed to read frame: %w", err)
			}

			// Exclude bones if configured
			if cfg.Converter.ExcludeBones {
				frame.PlayerBones = nil
			}

			if err := writer.WriteFrame(frame); err != nil {
				return nil, fmt.Errorf("failed to write frame: %w", err)
			}
			stats.FrameCount++
		}

	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}

	// Get output file size
	if outputInfo, err := os.Stat(outputFile); err == nil {
		stats.OutputSize = outputInfo.Size()
	}

	return stats, nil
}

func countFrames(filename string) (int, error) {
	format := getFileFormat(filename)

	switch format {
	case "echoreplay":
		reader, err := codecs.NewEchoReplayReader(filename)
		if err != nil {
			return 0, err
		}
		defer reader.Close()

		count := 0
		for reader.HasNext() {
			if _, err := reader.ReadFrame(); err != nil {
				if err == io.EOF {
					break
				}
				return 0, err
			}
			count++
		}
		return count, nil

	case "nevrcap":
		reader, err := codecs.NewNevrCapReader(filename)
		if err != nil {
			return 0, err
		}
		defer reader.Close()

		if _, err := reader.ReadHeader(); err != nil {
			return 0, err
		}

		count := 0
		for {
			if _, err := reader.ReadFrame(); err != nil {
				if err == io.EOF {
					break
				}
				return 0, err
			}
			count++
		}
		return count, nil

	default:
		return 0, fmt.Errorf("unknown format: %s", format)
	}
}

func discoverFiles() ([]string, error) {
	inputPath := cfg.Converter.InputFile

	fileInfo, err := os.Stat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("cannot access input path: %w", err)
	}

	if !fileInfo.IsDir() {
		if cfg.Converter.Recursive || cfg.Converter.Glob != "" {
			return nil, fmt.Errorf("--recursive and --glob can only be used with directory inputs")
		}
		return []string{inputPath}, nil
	}

	var files []string

	walkFunc := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if !cfg.Converter.Recursive && path != inputPath {
				return filepath.SkipDir
			}
			return nil
		}

		lowerPath := strings.ToLower(path)
		if !strings.HasSuffix(lowerPath, ".echoreplay") && !strings.HasSuffix(lowerPath, ".nevrcap") {
			return nil
		}

		if cfg.Converter.Glob != "" {
			matched, err := filepath.Match(cfg.Converter.Glob, filepath.Base(path))
			if err != nil {
				return fmt.Errorf("invalid glob pattern: %w", err)
			}
			if !matched {
				return nil
			}
		}

		files = append(files, path)
		return nil
	}

	if err := filepath.Walk(inputPath, walkFunc); err != nil {
		return nil, fmt.Errorf("error walking directory: %w", err)
	}

	return files, nil
}

func determineOutputFileForInput(inputFile string) (string, error) {
	if cfg.Converter.OutputFile != "" {
		outputDir := filepath.Dir(cfg.Converter.OutputFile)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create output directory: %w", err)
		}
		return cfg.Converter.OutputFile, nil
	}

	targetFormat := cfg.Converter.Format
	if targetFormat == "auto" {
		if strings.HasSuffix(strings.ToLower(inputFile), ".echoreplay") {
			targetFormat = "nevrcap"
		} else if strings.HasSuffix(strings.ToLower(inputFile), ".nevrcap") {
			targetFormat = "echoreplay"
		} else {
			return "", fmt.Errorf("cannot auto-detect target format for input file: %s", inputFile)
		}
	}

	inputBase := filepath.Base(inputFile)
	var outputName string

	switch targetFormat {
	case "echoreplay":
		if strings.HasSuffix(strings.ToLower(inputBase), ".nevrcap") {
			outputName = strings.TrimSuffix(inputBase, ".nevrcap") + ".echoreplay"
		} else {
			outputName = strings.TrimSuffix(inputBase, ".echoreplay") + "_converted.echoreplay"
		}
	case "nevrcap":
		if strings.HasSuffix(strings.ToLower(inputBase), ".echoreplay") {
			outputName = strings.TrimSuffix(inputBase, ".echoreplay") + ".nevrcap"
		} else {
			outputName = strings.TrimSuffix(inputBase, ".nevrcap") + "_converted.nevrcap"
		}
	default:
		return "", fmt.Errorf("unsupported target format: %s", targetFormat)
	}

	if err := os.MkdirAll(cfg.Converter.OutputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	return filepath.Join(cfg.Converter.OutputDir, outputName), nil
}

func validateRoundTrip(inputFile string) error {
	if !strings.HasSuffix(strings.ToLower(inputFile), ".echoreplay") {
		return fmt.Errorf("validation only supports .echoreplay files")
	}

	logger.Info("Starting round-trip validation", zap.String("file", inputFile))

	tempDir, err := os.MkdirTemp("", "nevr-validate-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	tempNevrcap := filepath.Join(tempDir, "temp.nevrcap")
	tempEchoreplay := filepath.Join(tempDir, "temp.echoreplay")

	logger.Info("Converting to nevrcap", zap.String("temp", tempNevrcap))
	if err := conversion.ConvertEchoReplayToNevrcap(inputFile, tempNevrcap); err != nil {
		return fmt.Errorf("failed to convert to nevrcap: %w", err)
	}

	logger.Info("Converting back to echoreplay", zap.String("temp", tempEchoreplay))
	if err := conversion.ConvertNevrcapToEchoReplay(tempNevrcap, tempEchoreplay); err != nil {
		return fmt.Errorf("failed to convert back to echoreplay: %w", err)
	}

	logger.Info("Reading original raw JSON frames")
	originalFrames, err := readRawJSONFrames(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read original frames: %w", err)
	}

	logger.Info("Reading round-trip raw JSON frames")
	roundtripFrames, err := readRawJSONFrames(tempEchoreplay)
	if err != nil {
		return fmt.Errorf("failed to read round-trip frames: %w", err)
	}

	logger.Info("Comparing frames",
		zap.Int("original_count", len(originalFrames)),
		zap.Int("roundtrip_count", len(roundtripFrames)))

	if len(originalFrames) != len(roundtripFrames) {
		return fmt.Errorf("frame count mismatch: original=%d, roundtrip=%d",
			len(originalFrames), len(roundtripFrames))
	}

	for i := range originalFrames {
		if err := compareJSONFrames(i, originalFrames[i], roundtripFrames[i]); err != nil {
			return fmt.Errorf("frame %d mismatch: %w", i, err)
		}
	}

	logger.Info("Round-trip validation successful",
		zap.Int("frames_validated", len(originalFrames)))

	return nil
}

type rawJSONFrame struct {
	timestamp   string
	sessionJSON []byte
	bonesJSON   []byte
}

func readRawJSONFrames(filename string) ([]*rawJSONFrame, error) {
	zipReader, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open echoreplay file: %w", err)
	}
	defer zipReader.Close()

	var replayFile *zip.File
	baseFilename := filepath.Base(filename)

	for _, file := range zipReader.File {
		if file.Name == baseFilename {
			replayFile = file
			break
		}
	}

	if replayFile == nil {
		for _, file := range zipReader.File {
			if filepath.Ext(file.Name) == ".echoreplay" {
				replayFile = file
				break
			}
		}
	}

	if replayFile == nil {
		return nil, fmt.Errorf("no .echoreplay file found in zip")
	}

	reader, err := replayFile.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	const maxScannerBuffer = 10 * 1024 * 1024
	scanner.Buffer(make([]byte, 64*1024), maxScannerBuffer)

	var frames []*rawJSONFrame

	for scanner.Scan() {
		line := scanner.Bytes()
		parts := bytes.Split(line, []byte("\t"))
		if len(parts) < 2 {
			continue
		}

		frame := &rawJSONFrame{
			timestamp:   string(parts[0]),
			sessionJSON: make([]byte, len(parts[1])),
		}
		copy(frame.sessionJSON, parts[1])

		if len(parts) > 2 && len(parts[2]) > 0 {
			bonesData := parts[2]
			if bonesData[0] == ' ' {
				bonesData = bonesData[1:]
			}
			if len(bonesData) > 0 {
				frame.bonesJSON = make([]byte, len(bonesData))
				copy(frame.bonesJSON, bonesData)
			}
		}

		frames = append(frames, frame)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	return frames, nil
}

func compareJSONFrames(frameNum int, original, roundtrip *rawJSONFrame) error {
	if original.timestamp != roundtrip.timestamp {
		return fmt.Errorf("timestamp mismatch: %q != %q", original.timestamp, roundtrip.timestamp)
	}

	if err := compareNormalizedJSON("session", original.sessionJSON, roundtrip.sessionJSON); err != nil {
		if cfg.Converter.Verbose {
			logger.Error("Session JSON mismatch",
				zap.Int("frame", frameNum),
				zap.Error(err))
		}
		return err
	}

	if (original.bonesJSON == nil) != (roundtrip.bonesJSON == nil) {
		return fmt.Errorf("bones presence mismatch: original=%v, roundtrip=%v",
			original.bonesJSON != nil, roundtrip.bonesJSON != nil)
	}

	if original.bonesJSON != nil {
		if err := compareNormalizedJSON("bones", original.bonesJSON, roundtrip.bonesJSON); err != nil {
			if cfg.Converter.Verbose {
				logger.Error("Bones JSON mismatch",
					zap.Int("frame", frameNum),
					zap.Error(err))
			}
			return err
		}
	}

	return nil
}

func compareNormalizedJSON(fieldName string, json1, json2 []byte) error {
	var obj1, obj2 interface{}

	if err := json.Unmarshal(json1, &obj1); err != nil {
		return fmt.Errorf("failed to parse original %s JSON: %w", fieldName, err)
	}

	if err := json.Unmarshal(json2, &obj2); err != nil {
		return fmt.Errorf("failed to parse roundtrip %s JSON: %w", fieldName, err)
	}

	normalized1, err := json.Marshal(obj1)
	if err != nil {
		return fmt.Errorf("failed to normalize original %s JSON: %w", fieldName, err)
	}

	normalized2, err := json.Marshal(obj2)
	if err != nil {
		return fmt.Errorf("failed to normalize roundtrip %s JSON: %w", fieldName, err)
	}

	if !bytes.Equal(normalized1, normalized2) {
		hash1 := sha256.Sum256(normalized1)
		hash2 := sha256.Sum256(normalized2)
		return fmt.Errorf("%s JSON differs (hash: %x vs %x)", fieldName, hash1[:8], hash2[:8])
	}

	return nil
}
