package main

import (
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
	"github.com/spf13/viper"
	"go.uber.org/zap"
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
	  agent convert --input game.echoreplay --progress`,
		RunE: runConverter,
	}

	// Converter-specific flags
	cmd.Flags().StringP("input", "i", "", "Input file path (.echoreplay or .nevrcap) (required)")
	cmd.Flags().StringP("output", "o", "", "Output file path (optional, format detected from extension)")
	cmd.Flags().String("output-dir", "./", "Output directory for converted files")
	cmd.Flags().StringP("format", "f", "auto", "Output format: auto, echoreplay, nevrcap")
	cmd.Flags().BoolP("verbose", "v", false, "Enable verbose logging")
	cmd.Flags().Bool("overwrite", false, "Overwrite existing output files")
	cmd.Flags().BoolP("progress", "p", false, "Show progress bar during conversion")

	cmd.MarkFlagRequired("input")

	// Bind flags to viper
	viper.BindPFlags(cmd.Flags())

	return cmd
}

func runConverter(cmd *cobra.Command, args []string) error {
	// Override config with command flags
	cfg.Converter.InputFile = viper.GetString("input")
	cfg.Converter.OutputFile = viper.GetString("output")
	cfg.Converter.OutputDir = viper.GetString("output-dir")
	cfg.Converter.Format = viper.GetString("format")
	cfg.Converter.Verbose = viper.GetBool("verbose")
	cfg.Converter.Overwrite = viper.GetBool("overwrite")
	showProgress := viper.GetBool("progress")

	// Validate configuration
	if err := cfg.ValidateConverterConfig(); err != nil {
		return err
	}

	if cfg.Converter.Verbose {
		logger.Info("Starting conversion",
			zap.String("input", cfg.Converter.InputFile),
			zap.String("output", cfg.Converter.OutputFile),
			zap.String("format", cfg.Converter.Format))
	}

	startTime := time.Now()

	// Determine output file path
	outputFile, err := determineOutputFile()
	if err != nil {
		return fmt.Errorf("failed to determine output file: %w", err)
	}

	if cfg.Converter.Verbose {
		logger.Info("Output file determined", zap.String("output", outputFile))
	}

	// Check if output file exists
	if _, err := os.Stat(outputFile); err == nil && !cfg.Converter.Overwrite {
		return fmt.Errorf("output file already exists (use --overwrite to overwrite): %s", outputFile)
	}

	// Perform conversion
	stats, err := convertFile(cfg.Converter.InputFile, outputFile, showProgress)
	if err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	// Report results
	duration := time.Since(startTime)
	logger.Info("Conversion completed successfully",
		zap.Int("frames", stats.FrameCount),
		zap.Duration("duration", duration),
		zap.Int64("input_size", stats.InputSize),
		zap.Int64("output_size", stats.OutputSize))

	if stats.InputSize > 0 {
		compressionRatio := float64(stats.OutputSize) / float64(stats.InputSize) * 100
		logger.Info("Compression ratio", zap.Float64("ratio", compressionRatio))
	}

	return nil
}

type ConversionStats struct {
	FrameCount int
	InputSize  int64
	OutputSize int64
}

func determineOutputFile() (string, error) {
	if cfg.Converter.OutputFile != "" {
		outputDir := filepath.Dir(cfg.Converter.OutputFile)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create output directory: %w", err)
		}
		return cfg.Converter.OutputFile, nil
	}

	// Determine target format
	targetFormat := cfg.Converter.Format
	if targetFormat == "auto" {
		if strings.HasSuffix(strings.ToLower(cfg.Converter.InputFile), ".echoreplay") {
			targetFormat = "nevrcap"
		} else if strings.HasSuffix(strings.ToLower(cfg.Converter.InputFile), ".nevrcap") {
			targetFormat = "echoreplay"
		} else {
			return "", fmt.Errorf("cannot auto-detect target format for input file: %s", cfg.Converter.InputFile)
		}
	}

	// Generate output filename
	inputBase := filepath.Base(cfg.Converter.InputFile)
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
		if showProgress {
			if err := convertEchoReplayToNevrcapWithProgress(inputFile, outputFile); err != nil {
				return nil, err
			}
		} else {
			if err := conversion.ConvertEchoReplayToNevrcap(inputFile, outputFile); err != nil {
				return nil, err
			}
		}
	} else if inputFormat == "nevrcap" && outputFormat == "echoreplay" {
		if showProgress {
			if err := convertNevrcapToEchoReplayWithProgress(inputFile, outputFile); err != nil {
				return nil, err
			}
		} else {
			if err := conversion.ConvertNevrcapToEchoReplay(inputFile, outputFile); err != nil {
				return nil, err
			}
		}
	} else if inputFormat == outputFormat {
		// Same format, just copy
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

// convertEchoReplayToNevrcapWithProgress converts with a progress bar
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

	// Count total frames first for progress bar
	totalFrames := 0
	countReader, err := codecs.NewEchoReplayReader(inputFile)
	if err == nil {
		for countReader.HasNext() {
			if _, err := countReader.ReadFrame(); err != nil {
				break
			}
			totalFrames++
		}
		countReader.Close()
	}

	bar := progressbar.NewOptions(totalFrames,
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

	for reader.HasNext() {
		frame, err := reader.ReadFrame()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		if err := writer.WriteFrame(frame); err != nil {
			return fmt.Errorf("failed to write frame: %w", err)
		}

		bar.Add(1)
	}

	fmt.Println() // New line after progress bar
	return nil
}

// convertNevrcapToEchoReplayWithProgress converts with a progress bar
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

	// Count total frames first for progress bar
	totalFrames := 0
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

	bar := progressbar.NewOptions(totalFrames,
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

	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		if err := writer.WriteFrame(frame); err != nil {
			return fmt.Errorf("failed to write frame: %w", err)
		}

		bar.Add(1)
	}

	fmt.Println() // New line after progress bar
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

		// Skip header
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
