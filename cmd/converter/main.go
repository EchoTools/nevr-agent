package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/echotools/nevrcap"
)

const (
	// File extension constants
	EchoReplayExt    = ".echoreplay"
	NevrCapExt       = ".nevrcap.echoreplay"
	DefaultOutputDir = "./"
)

// ConverterConfig holds configuration for the converter
type ConverterConfig struct {
	InputFile       string
	OutputFile      string
	OutputDir       string
	Verbose         bool
	OverwriteMode   bool
	ExcludeBoneData bool
}

func main() {
	config := parseFlags()

	if err := runConverter(config); err != nil {
		log.Fatalf("Converter failed: %v", err)
	}
}

// parseFlags parses command line flags and returns configuration
func parseFlags() *ConverterConfig {
	config := &ConverterConfig{}

	flag.StringVar(&config.InputFile, "input", "", "Input .echoreplay file path (required)")
	flag.StringVar(&config.OutputFile, "output", "", "Output .nevrcap.echoreplay file path (optional)")
	flag.StringVar(&config.OutputDir, "output-dir", DefaultOutputDir, "Output directory for converted files")
	flag.BoolVar(&config.Verbose, "verbose", false, "Enable verbose logging")
	flag.BoolVar(&config.OverwriteMode, "overwrite", false, "Overwrite existing output files")
	flag.BoolVar(&config.ExcludeBoneData, "exclude-bone-data", false, "Exclude bone data from converted files (reduces file size)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Converts .echoreplay files to .nevrcap.echoreplay format\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -input game.echoreplay\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -input game.echoreplay -output converted_game.nevrcap.echoreplay\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -input game.echoreplay -output-dir ./output -verbose\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -input game.echoreplay -exclude-bone-data -verbose\n", os.Args[0])
	}

	flag.Parse()

	// Validate required flags
	if config.InputFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -input flag is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	return config
}

// runConverter executes the file conversion process
func runConverter(config *ConverterConfig) error {
	startTime := time.Now()

	if config.Verbose {
		log.Printf("Starting conversion process...")
		log.Printf("Input file: %s", config.InputFile)
	}

	// Validate input file
	if err := validateInputFile(config.InputFile); err != nil {
		return fmt.Errorf("input validation failed: %w", err)
	}

	// Determine output file path
	outputFile, err := determineOutputFile(config)
	if err != nil {
		return fmt.Errorf("output file determination failed: %w", err)
	}

	if config.Verbose {
		log.Printf("Output file: %s", outputFile)
	}

	// Check if output file exists and handle overwrite mode
	if err := handleOutputFileExistence(outputFile, config.OverwriteMode); err != nil {
		return err
	}

	// Perform the conversion
	stats, err := convertFile(config.InputFile, outputFile, config)
	if err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	// Report results
	duration := time.Since(startTime)
	log.Printf("Conversion completed successfully!")
	log.Printf("Processed %d frames in %v", stats.FrameCount, duration)
	log.Printf("Input file size: %d bytes", stats.InputSize)
	log.Printf("Output file size: %d bytes", stats.OutputSize)
	if stats.InputSize > 0 {
		compressionRatio := float64(stats.OutputSize) / float64(stats.InputSize) * 100
		log.Printf("Compression ratio: %.2f%%", compressionRatio)
	}
	log.Printf("Processing rate: %.2f frames/sec", float64(stats.FrameCount)/duration.Seconds())

	return nil
}

// ConversionStats holds statistics about the conversion process
type ConversionStats struct {
	FrameCount int
	InputSize  int64
	OutputSize int64
}

// convertFile performs the actual file conversion
func convertFile(inputFile, outputFile string, config *ConverterConfig) (*ConversionStats, error) {
	stats := &ConversionStats{}

	// Get input file size
	if inputInfo, err := os.Stat(inputFile); err == nil {
		stats.InputSize = inputInfo.Size()
	}

	// Create reader for input file
	reader, err := nevrcap.NewEchoReplayFileReader(inputFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create input reader: %w", err)
	}
	defer reader.Close()

	// Create writer for output file
	writer, err := nevrcap.NewEchoReplayCodecWriter(outputFile)
	if err != nil {
		reader.Close()
		return nil, fmt.Errorf("failed to create output writer: %w", err)
	}
	defer writer.Close()

	if config.Verbose {
		log.Printf("Starting frame-by-frame conversion...")
		if config.ExcludeBoneData {
			log.Printf("Bone data will be excluded from output")
		}
	}

	// Read and write frames
	frameCount := 0
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			if err == io.EOF {
				break // End of file reached
			}
			return nil, fmt.Errorf("failed to read frame %d: %w", frameCount+1, err)
		}

		// Exclude bone data if requested
		if config.ExcludeBoneData {
			frame.PlayerBones = nil
		}

		if err := writer.WriteFrame(frame); err != nil {
			return nil, fmt.Errorf("failed to write frame %d: %w", frameCount+1, err)
		}

		frameCount++

		// Log progress for large files
		if config.Verbose && frameCount%1000 == 0 {
			log.Printf("Processed %d frames... (buffer size: %d bytes)", frameCount, writer.GetBufferSize())
		}
	}

	stats.FrameCount = frameCount

	if config.Verbose {
		log.Printf("Finalizing output file...")
	}

	// Close writer to finalize the output
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize output file: %w", err)
	}

	// Get output file size
	if outputInfo, err := os.Stat(outputFile); err == nil {
		stats.OutputSize = outputInfo.Size()
	}

	return stats, nil
}

// validateInputFile checks if the input file is valid
func validateInputFile(inputFile string) error {
	// Check if file exists
	info, err := os.Stat(inputFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("input file does not exist: %s", inputFile)
		}
		return fmt.Errorf("cannot access input file: %w", err)
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("input is not a regular file: %s", inputFile)
	}

	// Check file extension
	if !strings.HasSuffix(strings.ToLower(inputFile), EchoReplayExt) {
		return fmt.Errorf("input file must have %s extension", EchoReplayExt)
	}

	// Check file size
	if info.Size() == 0 {
		return fmt.Errorf("input file is empty: %s", inputFile)
	}

	return nil
}

// determineOutputFile determines the output file path based on configuration
func determineOutputFile(config *ConverterConfig) (string, error) {
	// If output file is explicitly specified, use it
	if config.OutputFile != "" {
		// Ensure output directory exists
		outputDir := filepath.Dir(config.OutputFile)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create output directory: %w", err)
		}
		return config.OutputFile, nil
	}

	// Generate output filename based on input filename
	inputBase := filepath.Base(config.InputFile)
	inputName := strings.TrimSuffix(inputBase, EchoReplayExt)
	outputName := inputName + NevrCapExt

	// Ensure output directory exists
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	return filepath.Join(config.OutputDir, outputName), nil
}

// handleOutputFileExistence checks if output file exists and handles overwrite mode
func handleOutputFileExistence(outputFile string, overwriteMode bool) error {
	if _, err := os.Stat(outputFile); err == nil {
		if !overwriteMode {
			return fmt.Errorf("output file already exists (use -overwrite to overwrite): %s", outputFile)
		}
		log.Printf("Warning: Overwriting existing file: %s", outputFile)
	}
	return nil
}
