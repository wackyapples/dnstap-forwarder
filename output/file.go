package output

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dnstap-forwarder/config"
	"dnstap-forwarder/events"
)

// FileWriter handles writing events to rotating files
type FileWriter struct {
	BaseWriter
	currentFile *os.File
	currentSize int64
	fileDir     string
	fileBase    string
}

// NewFileWriter creates a new file writer
func NewFileWriter(config config.Config, hostname string) (*FileWriter, error) {
	fw := &FileWriter{
		BaseWriter: BaseWriter{
			config:   config,
			buffer:   make([]*events.DNSEvent, 0, config.BufferSize),
			hostname: hostname,
		},
	}

	// Parse the output path to get directory and base filename
	fw.fileDir = filepath.Dir(config.OutputPath)
	fw.fileBase = filepath.Base(config.OutputPath)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(fw.fileDir, 0755); err != nil {
		return nil, fmt.Errorf("creating directory: %w", err)
	}

	if err := fw.openFile(); err != nil {
		return nil, err
	}

	return fw, nil
}

// openFile creates and opens a new log file
func (fw *FileWriter) openFile() error {
	// Close current file if open
	if fw.currentFile != nil {
		fw.currentFile.Close()
	}

	// Generate filename with timestamp
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s-%s", strings.TrimSuffix(fw.fileBase, filepath.Ext(fw.fileBase)), timestamp)
	if ext := filepath.Ext(fw.fileBase); ext != "" {
		filename += ext
	}

	filePath := filepath.Join(fw.fileDir, filename)

	// Create new file
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", filePath, err)
	}

	fw.currentFile = file
	fw.writer = bufio.NewWriterSize(file, 64*1024) // 64KB buffer
	fw.currentSize = 0

	// Get current file size
	if stat, err := file.Stat(); err == nil {
		fw.currentSize = stat.Size()
	}

	log.Printf("Opened new log file: %s", filePath)
	return nil
}

// AddEvent adds an event to the buffer
func (fw *FileWriter) AddEvent(event *events.DNSEvent) error {
	fw.buffer = append(fw.buffer, event)

	if len(fw.buffer) >= fw.config.BatchSize {
		return fw.Flush()
	}
	return nil
}

// Flush writes all buffered events to the file
func (fw *FileWriter) Flush() error {
	if len(fw.buffer) == 0 {
		return nil
	}

	for _, event := range fw.buffer {
		message := createMessage(event, fw.hostname, fw.config.OutputFormat)
		if err := fw.writeMessage(message, fw.reconnect); err != nil {
			return err
		}
		fw.currentSize += int64(len(message))
	}

	if err := fw.writer.Flush(); err != nil {
		return fmt.Errorf("flushing buffer: %w", err)
	}

	// Check if we need to rotate the file
	if fw.currentSize >= fw.config.MaxFileSize {
		if err := fw.rotateFile(); err != nil {
			return fmt.Errorf("rotating file: %w", err)
		}
	}

	fw.eventCount += int64(len(fw.buffer))
	if fw.config.LogLevel == "debug" || fw.eventCount%1000 == 0 {
		log.Printf("Wrote %d events to file (total: %d)", len(fw.buffer), fw.eventCount)
	}

	fw.buffer = fw.buffer[:0] // Clear the buffer
	return nil
}

// rotateFile rotates the current file and cleans up old files
func (fw *FileWriter) rotateFile() error {
	log.Printf("Rotating file (current size: %d bytes)", fw.currentSize)

	// Open a new file
	if err := fw.openFile(); err != nil {
		return err
	}

	// Clean up old files
	if err := fw.cleanupOldFiles(); err != nil {
		log.Printf("Warning: failed to cleanup old files: %v", err)
	}

	return nil
}

// cleanupOldFiles removes old log files beyond the maximum count
func (fw *FileWriter) cleanupOldFiles() error {
	// Get all log files in the directory
	files, err := os.ReadDir(fw.fileDir)
	if err != nil {
		return fmt.Errorf("reading directory: %w", err)
	}

	var logFiles []string
	baseName := strings.TrimSuffix(fw.fileBase, filepath.Ext(fw.fileBase))

	for _, file := range files {
		if !file.IsDir() && strings.HasPrefix(file.Name(), baseName) {
			logFiles = append(logFiles, filepath.Join(fw.fileDir, file.Name()))
		}
	}

	// Sort files by modification time (oldest first)
	sort.Slice(logFiles, func(i, j int) bool {
		statI, errI := os.Stat(logFiles[i])
		statJ, errJ := os.Stat(logFiles[j])
		if errI != nil || errJ != nil {
			return false
		}
		return statI.ModTime().Before(statJ.ModTime())
	})

	// Remove files beyond the maximum count
	if len(logFiles) > fw.config.MaxFiles {
		filesToRemove := logFiles[:len(logFiles)-fw.config.MaxFiles]
		for _, file := range filesToRemove {
			if err := os.Remove(file); err != nil {
				log.Printf("Warning: failed to remove old file %s: %v", file, err)
			} else {
				log.Printf("Removed old log file: %s", file)
			}
		}
	}

	return nil
}

// reconnect attempts to reopen the current file
func (fw *FileWriter) reconnect() error {
	log.Printf("Attempting to reopen file...")
	return fw.openFile()
}

// Close closes the file writer
func (fw *FileWriter) Close() error {
	if err := fw.Flush(); err != nil {
		log.Printf("Error flushing on close: %v", err)
	}

	if fw.currentFile != nil {
		fw.currentFile.Close()
	}

	return nil
}
