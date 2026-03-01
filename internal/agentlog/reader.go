package agentlog

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
)

const (
	initialBufSize = 64 * 1024
	maxBufSize     = 64 * 1024 * 1024
)

// ReadEntries reads all JSONL entries from a file.
func ReadEntries(path string) ([]Entry, int64, error) {
	return ReadEntriesFrom(path, 0)
}

// ReadEntriesFrom reads JSONL entries starting from a byte offset.
func ReadEntriesFrom(path string, offset int64) ([]Entry, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, 0, err
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, initialBufSize), maxBufSize)

	var entries []Entry
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		entry.Raw = make(json.RawMessage, len(line))
		copy(entry.Raw, line)
		entries = append(entries, entry)
	}

	fi, err := f.Stat()
	var finalOffset int64
	if err == nil {
		finalOffset = fi.Size()
	}

	return entries, finalOffset, scanner.Err()
}
