package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Parse command line flags
	pipePath := flag.String("pipe", "/var/run/dnstap.pipe", "Path to the named pipe to read from")
	showRaw := flag.Bool("raw", false, "Show raw syslog format instead of just JSON")
	flag.Parse()

	log.Printf("Starting pipe reader for: %s", *pipePath)
	log.Printf("Press Ctrl+C to stop")

	// Check if pipe exists
	if _, err := os.Stat(*pipePath); os.IsNotExist(err) {
		log.Fatalf("Pipe does not exist: %s", *pipePath)
	}

	// Open the named pipe for reading
	pipeFile, err := os.OpenFile(*pipePath, os.O_RDONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open pipe: %v", err)
	}
	defer pipeFile.Close()

	log.Printf("Successfully opened pipe for reading")

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create a channel to signal when reading is done
	done := make(chan bool)

	// Start reading from pipe in a goroutine
	go func() {
		scanner := bufio.NewScanner(pipeFile)
		messageCount := 0

		for scanner.Scan() {
			messageCount++
			line := scanner.Text()

			if *showRaw {
				// Show the full syslog message
				fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), line)
			} else {
				// Extract and show just the JSON part
				if jsonStart := findJSONStart(line); jsonStart != -1 {
					jsonPart := line[jsonStart:]
					fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), jsonPart)
				} else {
					fmt.Printf("[%s] (No JSON found) %s\n", time.Now().Format("15:04:05"), line)
				}
			}

			// Log progress every 100 messages
			if messageCount%100 == 0 {
				log.Printf("Processed %d messages", messageCount)
			}
		}

		if err := scanner.Err(); err != nil {
			log.Printf("Error reading from pipe: %v", err)
		}

		log.Printf("Pipe reader finished after processing %d messages", messageCount)
		done <- true
	}()

	// Wait for either signal or completion
	select {
	case <-sigChan:
		log.Println("Received shutdown signal")
	case <-done:
		log.Println("Reading completed")
	}
}

// findJSONStart finds the start of JSON content in a syslog message
// Syslog format: <priority>timestamp hostname program[pid]: JSON
func findJSONStart(line string) int {
	// Look for the colon that separates the syslog header from the message
	for i := 0; i < len(line); i++ {
		if line[i] == ':' {
			// Skip the colon and any following whitespace
			for j := i + 1; j < len(line); j++ {
				if line[j] != ' ' && line[j] != '\t' {
					return j
				}
			}
		}
	}
	return -1
}
