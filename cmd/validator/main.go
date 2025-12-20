// Package main provides a validator for the EchoReplay encoder/decoder codec.
// It validates that the codec can correctly round-trip JSON data by:
// 1. Manually parsing each line to create a "control" map
// 2. Using the codec to parse and re-encode the data
// 3. Comparing the results, ignoring trivial rounding differences
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"

	"github.com/echotools/nevr-capture/v3/pkg/codecs"
	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protojson"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <echoreplay_file>\n", os.Args[0])
		os.Exit(1)
	}

	filename := os.Args[1]
	if err := validateEchoReplayFile(filename); err != nil {
		fmt.Fprintf(os.Stderr, "Validation failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Validation successful!")
}

// validateEchoReplayFile validates an .echoreplay file
func validateEchoReplayFile(filename string) error {
	// Try to open as zip first, fall back to uncompressed
	var manualReader io.ReadCloser
	var codec *codecs.EchoReplay
	var err error

	zipReader, zipErr := zip.OpenReader(filename)
	if zipErr == nil {
		// It's a zip file
		defer zipReader.Close()

		// Find the echoreplay file inside
		var replayFile *zip.File
		baseFilename := filepath.Base(filename)
		if ext := filepath.Ext(baseFilename); ext != "" {
			baseFilename = baseFilename[:len(baseFilename)-len(ext)]
		}

		for _, file := range zipReader.File {
			if file.Name == baseFilename || filepath.Ext(file.Name) == ".echoreplay" {
				replayFile = file
				break
			}
		}

		if replayFile == nil {
			// If no matching file found, use the first file
			if len(zipReader.File) > 0 {
				replayFile = zipReader.File[0]
			} else {
				return fmt.Errorf("no files found in zip")
			}
		}

		// Open the replay file for manual parsing
		manualReader, err = replayFile.Open()
		if err != nil {
			return fmt.Errorf("failed to open replay file for manual parsing: %w", err)
		}
		defer manualReader.Close()

		// Use the codec
		codec, err = codecs.NewEchoReplayReader(filename)
		if err != nil {
			return fmt.Errorf("failed to create codec reader: %w", err)
		}
		defer codec.Close()
	} else {
		// Not a zip file, try uncompressed
		file, err := os.Open(filename)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		manualReader = file

		// For uncompressed files, we can't use the codec's reader directly
		// since it expects zip format. We'll create a mock comparison instead.
		fmt.Println("Note: File is uncompressed. Comparing original JSON vs protojson re-encoding.")
	}
	defer manualReader.Close()

	// Unmarshaler for parsing original JSON into protobufs
	unmarshaler := &protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}

	// Read all lines manually
	scanner := bufio.NewScanner(manualReader)
	// Increase buffer size for large JSON lines
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)
	lineNum := 0
	errorCount := 0
	maxErrors := 10 // Stop after this many errors

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Step 1: Manually parse the line to get original JSON as map
		controlSession, controlBones, err := manuallyParseLine(line)
		if err != nil {
			fmt.Printf("Line %d: Failed to manually parse: %v\n", lineNum, err)
			errorCount++
			if errorCount >= maxErrors {
				return fmt.Errorf("too many errors, stopping")
			}
			continue
		}

		// Step 2: Parse JSON into protobuf and re-encode
		var frame *telemetry.LobbySessionStateFrame
		if codec != nil {
			// Use codec for zip files
			frame, err = codec.ReadFrame()
			if err != nil {
				if err == io.EOF {
					return fmt.Errorf("codec returned EOF at line %d, but manual parser found data", lineNum)
				}
				fmt.Printf("Line %d: Codec failed to read frame: %v\n", lineNum, err)
				errorCount++
				if errorCount >= maxErrors {
					return fmt.Errorf("too many errors, stopping")
				}
				continue
			}
		} else {
			// For uncompressed files, manually parse into protobuf
			frame, err = parseLineToFrame(line, unmarshaler)
			if err != nil {
				fmt.Printf("Line %d: Failed to parse to frame: %v\n", lineNum, err)
				errorCount++
				if errorCount >= maxErrors {
					return fmt.Errorf("too many errors, stopping")
				}
				continue
			}
		}

		// Re-encode the frame using codec's marshaler approach
		codecSession, codecBones, err := reEncodeWithCodec(frame)
		if err != nil {
			fmt.Printf("Line %d: Failed to re-encode frame: %v\n", lineNum, err)
			errorCount++
			if errorCount >= maxErrors {
				return fmt.Errorf("too many errors, stopping")
			}
			continue
		}

		// Step 3: Compare control vs codec output
		// Standard tolerance-based comparison
		sessionDiffs := compareWithTolerance(controlSession, codecSession, "session", 1e-6)
		bonesDiffs := compareWithTolerance(controlBones, codecBones, "user_bones", 1e-6)

		// Additional comparison using cmp.Diff for deep structural differences
		cmpSessionDiff := cmp.Diff(controlSession, codecSession)
		cmpBonesDiff := cmp.Diff(controlBones, codecBones)

		if len(sessionDiffs) > 0 || len(bonesDiffs) > 0 || cmpSessionDiff != "" || cmpBonesDiff != "" {
			fmt.Printf("Line %d: Differences found:\n", lineNum)
			for _, diff := range sessionDiffs {
				fmt.Printf("  Session: %s\n", diff)
			}
			for _, diff := range bonesDiffs {
				fmt.Printf("  Bones: %s\n", diff)
			}
			if cmpSessionDiff != "" {
				fmt.Printf("  cmp.Session diff:\n%s\n", cmpSessionDiff)
			}
			if cmpBonesDiff != "" {
				fmt.Printf("  cmp.Bones diff:\n%s\n", cmpBonesDiff)
			}
			errorCount++
			if errorCount >= maxErrors {
				return fmt.Errorf("too many errors, stopping")
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	fmt.Printf("Processed %d lines with %d errors\n", lineNum, errorCount)
	if errorCount > 0 {
		return fmt.Errorf("%d validation errors found", errorCount)
	}
	return nil
}

// manuallyParseLine parses a single line manually without using the codec
func manuallyParseLine(line []byte) (session map[string]any, bones map[string]any, err error) {
	// Format: timestamp\tsession_json\t user_bones_json
	parts := bytes.Split(line, []byte("\t"))
	if len(parts) < 2 {
		return nil, nil, fmt.Errorf("invalid line format: expected at least 2 tab-separated parts")
	}

	// Parse session JSON (second part)
	session = make(map[string]any)
	if err := json.Unmarshal(parts[1], &session); err != nil {
		return nil, nil, fmt.Errorf("failed to parse session JSON: %w", err)
	}

	// Parse bones JSON if present (third part, may have leading space)
	bones = make(map[string]any)
	if len(parts) > 2 {
		bonesData := parts[2]
		// Skip leading space if present
		if len(bonesData) > 0 && bonesData[0] == ' ' {
			bonesData = bonesData[1:]
		}
		if len(bonesData) > 0 {
			if err := json.Unmarshal(bonesData, &bones); err != nil {
				return nil, nil, fmt.Errorf("failed to parse bones JSON: %w", err)
			}
		}
	}

	return session, bones, nil
}

// parseLineToFrame parses a line directly into a LobbySessionStateFrame
func parseLineToFrame(line []byte, unmarshaler *protojson.UnmarshalOptions) (*telemetry.LobbySessionStateFrame, error) {
	// Format: timestamp\tsession_json\t user_bones_json
	parts := bytes.Split(line, []byte("\t"))
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid line format: expected at least 2 tab-separated parts")
	}

	frame := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{},
	}

	// Parse session JSON (second part)
	if err := unmarshaler.Unmarshal(parts[1], frame.Session); err != nil {
		return nil, fmt.Errorf("failed to parse session JSON: %w", err)
	}

	// Parse bones JSON if present (third part, may have leading space)
	if len(parts) > 2 {
		bonesData := parts[2]
		// Skip leading space if present
		if len(bonesData) > 0 && bonesData[0] == ' ' {
			bonesData = bonesData[1:]
		}
		if len(bonesData) > 0 {
			frame.PlayerBones = &apigame.PlayerBonesResponse{}
			if err := unmarshaler.Unmarshal(bonesData, frame.PlayerBones); err != nil {
				return nil, fmt.Errorf("failed to parse bones JSON: %w", err)
			}
		}
	}

	return frame, nil
}

// reEncodeFrameWithProtobuf takes a decoded frame and re-encodes it using the same marshaler settings as the codec
func reEncodeWithCodec(frame *telemetry.LobbySessionStateFrame) (session map[string]any, bones map[string]any, err error) {
	if frame == nil {
		return nil, nil, fmt.Errorf("nil frame")
	}

	// Use protojson marshaler with same settings as codec
	marshaler := &protojson.MarshalOptions{
		UseProtoNames:   false,
		UseEnumNumbers:  true,
		EmitUnpopulated: true,
	}

	// Re-marshal session to JSON then apply uint64 fix, then to map[string]any for comparison
	sessionBytes, err := marshaler.Marshal(frame.Session)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal session: %w", err)
	}
	// Apply the same fix as the codec
	sessionBytes = codecs.FixProtojsonUint64Encoding(sessionBytes)
	session = make(map[string]any)
	if err := json.Unmarshal(sessionBytes, &session); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	// Re-marshal bones if present
	bones = make(map[string]any)
	if frame.PlayerBones != nil {
		bonesBytes, err := marshaler.Marshal(frame.PlayerBones)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal bones: %w", err)
		}
		// Apply the same fix as the codec
		bonesBytes = codecs.FixProtojsonUint64Encoding(bonesBytes)
		if err := json.Unmarshal(bonesBytes, &bones); err != nil {
			return nil, nil, fmt.Errorf("failed to unmarshal bones: %w", err)
		}
	}

	return session, bones, nil
}

func reEncodeTheFrameWithJsonPackage(frame *telemetry.LobbySessionStateFrame) (session map[string]any, bones map[string]any, err error) {
	if frame == nil {
		return nil, nil, fmt.Errorf("nil frame")
	}

	// Marshal Session using encoding/json with explicit options
	session = make(map[string]any)
	sessionBytes, err := json.MarshalIndent(frame.Session, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal session with encoding/json: %w", err)
	}
	if err := json.Unmarshal(sessionBytes, &session); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal session JSON: %w", err)
	}

	// Marshal PlayerBones using encoding/json with explicit options
	bones = make(map[string]any)
	if frame.PlayerBones != nil {
		bonesBytes, err := json.MarshalIndent(frame.PlayerBones, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal bones with encoding/json: %w", err)
		}
		if err := json.Unmarshal(bonesBytes, &bones); err != nil {
			return nil, nil, fmt.Errorf("failed to unmarshal bones JSON: %w", err)
		}
	}

	return session, bones, nil
}

// compareWithTolerance compares two maps, ignoring trivial floating point differences
func compareWithTolerance(a, b map[string]any, prefix string, tolerance float64) []string {
	var diffs []string
	compareRecursive(a, b, prefix, tolerance, &diffs)
	return diffs
}

// compareRecursive recursively compares two values
func compareRecursive(a, b any, path string, tolerance float64, diffs *[]string) {
	if a == nil && b == nil {
		return
	}
	if a == nil || b == nil {
		*diffs = append(*diffs, fmt.Sprintf("%s: one is nil (a=%v, b=%v)", path, a, b))
		return
	}

	aType := reflect.TypeOf(a)
	bType := reflect.TypeOf(b)

	// Handle type differences, but allow float64/int conversions and string/number conversions
	// (protojson encodes uint64/int64 as strings)
	if aType != bType {
		// Try to convert to comparable numbers
		aNum, aIsNum := toFloat64(a)
		bNum, bIsNum := toFloat64(b)
		if aIsNum && bIsNum {
			if !floatEquals(aNum, bNum, tolerance) {
				*diffs = append(*diffs, fmt.Sprintf("%s: numeric mismatch (a=%v, b=%v)", path, a, b))
			}
			return
		}

		*diffs = append(*diffs, fmt.Sprintf("%s: type mismatch (a=%T [%v], b=%T [%v])", path, a, a, b, b))
		return
	}

	switch aVal := a.(type) {
	case map[string]any:
		bVal := b.(map[string]any)
		compareMapWithTolerance(aVal, bVal, path, tolerance, diffs)

	case []any:
		bVal := b.([]any)
		if len(aVal) != len(bVal) {
			*diffs = append(*diffs, fmt.Sprintf("%s: slice length mismatch (a=%d, b=%d)", path, len(aVal), len(bVal)))
			return
		}
		for i := range aVal {
			compareRecursive(aVal[i], bVal[i], fmt.Sprintf("%s[%d]", path, i), tolerance, diffs)
		}

	case float64:
		bVal := b.(float64)
		if !floatEquals(aVal, bVal, tolerance) {
			*diffs = append(*diffs, fmt.Sprintf("%s: float mismatch (a=%v, b=%v, diff=%v)", path, aVal, bVal, math.Abs(aVal-bVal)))
		}

	case string:
		bVal := b.(string)
		if aVal != bVal {
			*diffs = append(*diffs, fmt.Sprintf("%s: string mismatch (a=%q, b=%q)", path, aVal, bVal))
		}

	case bool:
		bVal := b.(bool)
		if aVal != bVal {
			*diffs = append(*diffs, fmt.Sprintf("%s: bool mismatch (a=%v, b=%v)", path, aVal, bVal))
		}

	default:
		if !reflect.DeepEqual(a, b) {
			*diffs = append(*diffs, fmt.Sprintf("%s: value mismatch (a=%v, b=%v)", path, a, b))
		}
	}
}

// compareMapWithTolerance compares two maps
func compareMapWithTolerance(a, b map[string]any, path string, tolerance float64, diffs *[]string) {
	// Check all keys in a
	for k, av := range a {
		bv, exists := b[k]
		keyPath := path + "." + k
		if !exists {
			// Check if value is zero/empty - those might be omitted
			if isZeroValue(av) {
				continue
			}
			*diffs = append(*diffs, fmt.Sprintf("%s: key missing in b", keyPath))
			continue
		}
		compareRecursive(av, bv, keyPath, tolerance, diffs)
	}

	// Check for extra keys in b
	for k, bv := range b {
		if _, exists := a[k]; !exists {
			// Check if value is zero/empty - those might be omitted
			if isZeroValue(bv) {
				continue
			}
			*diffs = append(*diffs, fmt.Sprintf("%s.%s: key missing in a", path, k))
		}
	}
}

// isZeroValue checks if a value is a zero/default value that might be omitted
func isZeroValue(v any) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case float64:
		return val == 0
	case int:
		return val == 0
	case string:
		return val == ""
	case bool:
		return !val
	case []any:
		return len(val) == 0
	case map[string]any:
		return len(val) == 0
	}
	return false
}

// toFloat64 tries to convert a value to float64
func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case float32:
		return float64(val), true
	}
	return 0, false
}

// floatEquals compares two floats with tolerance for rounding errors
func floatEquals(a, b, tolerance float64) bool {
	// Handle special cases
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	if math.IsInf(a, 1) && math.IsInf(b, 1) {
		return true
	}
	if math.IsInf(a, -1) && math.IsInf(b, -1) {
		return true
	}

	// For zero values
	if a == 0 && b == 0 {
		return true
	}

	// Absolute difference check for small numbers
	diff := math.Abs(a - b)
	if diff <= tolerance {
		return true
	}

	// Relative difference check for larger numbers
	maxAbs := math.Max(math.Abs(a), math.Abs(b))
	if maxAbs > 0 && diff/maxAbs <= tolerance {
		return true
	}

	return false
}
