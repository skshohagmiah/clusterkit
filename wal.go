package clusterkit

import (
	"encoding/json"
	"fmt"
	"os"
)

// writeWAL appends a WAL entry to the log file
func (ck *ClusterKit) writeWAL(entry WALEntry) error {
	file, err := os.OpenFile(ck.walFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open WAL file: %v", err)
	}
	defer file.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal WAL entry: %v", err)
	}

	// Append newline for easy reading
	data = append(data, '\n')

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write WAL entry: %v", err)
	}

	return nil
}

// ReadWAL reads all WAL entries from the log file
func (ck *ClusterKit) ReadWAL() ([]WALEntry, error) {
	data, err := os.ReadFile(ck.walFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []WALEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read WAL file: %v", err)
	}

	var entries []WALEntry
	lines := []byte{}
	for _, b := range data {
		if b == '\n' {
			if len(lines) > 0 {
				var entry WALEntry
				if err := json.Unmarshal(lines, &entry); err == nil {
					entries = append(entries, entry)
				}
				lines = []byte{}
			}
		} else {
			lines = append(lines, b)
		}
	}

	return entries, nil
}

// ClearWAL removes the WAL file
func (ck *ClusterKit) ClearWAL() error {
	if err := os.Remove(ck.walFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear WAL: %v", err)
	}
	return nil
}
