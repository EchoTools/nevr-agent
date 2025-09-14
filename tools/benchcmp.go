package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
)

// benchcmp: minimal tool to extract benchmark lines and print them.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: benchcmp <bench-output-file>")
		os.Exit(2)
	}
	file := os.Args[1]
	f, err := os.Open(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	r := regexp.MustCompile(`^Benchmark`) // lines starting with Benchmark
	for scanner.Scan() {
		line := scanner.Text()
		if r.MatchString(line) {
			fmt.Println(line)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
