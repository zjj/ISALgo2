package main

import (
	"fmt"
	"log"
	"os"

	isal "github.com/zjj/ISALgo2/v2"
)

func main() {
	// Check if a filename was provided
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <input_file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s file.txt\n", os.Args[0])
		os.Exit(1)
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	err = isal.CompressCopyLevel(file, os.Stdout, 0) // Use level 0 for now
	if err != nil {
		log.Fatalf("Compression failed: %v", err)
	}
}
