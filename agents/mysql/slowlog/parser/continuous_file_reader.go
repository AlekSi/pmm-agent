// pmm-agent
// Copyright (C) 2018 Percona LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package parser

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"sync"
	"time"
)

const readerBufSize = 16 * 1024

// ContinuousFileReader reads lines from the single file across renames, truncations and symlink changes.
type ContinuousFileReader struct {
	filename string
	l        Logger

	// file Read/Close calls must be synchronized
	m      sync.Mutex
	closed bool
	f      *os.File
	r      *bufio.Reader

	sleep time.Duration // for testing only
}

// NewContinuousFileReader creates new ContinuousFileReader.
func NewContinuousFileReader(filename string, l Logger) (*ContinuousFileReader, error) {
	f, err := os.Open(filename) //nolint:gosec
	if err != nil {
		return nil, err
	}

	if _, err = f.Seek(0, io.SeekEnd); err != nil {
		l.Warnf("Failed to seek file to the end: %s.", err)
	}

	return &ContinuousFileReader{
		filename: filename,
		l:        l,
		f:        f,
		r:        bufio.NewReaderSize(f, readerBufSize),
		sleep:    time.Second,
	}, nil
}

// NextLine implements Reader interface.
func (r *ContinuousFileReader) NextLine() (string, error) {
	r.m.Lock()
	defer r.m.Unlock()

	var line string
	for {
		l, err := r.r.ReadString('\n')
		r.l.Tracef("ReadLine: %q %v", l, err)
		line += l

		switch {
		case err == nil:
			// Full line successfully read - return it.
			return line, nil

		case r.closed:
			// If file is closed, err would be os.PathError{"read", filename, os.ErrClosed}.
			// Return simple io.EOF instead.
			return line, io.EOF

		case err != io.EOF:
			// Return unexpected error as is.
			return line, err

		default:
			// err is io.EOF, but reader is not closed - reopen or sleep.
			needsReopen := r.needsReopen()
			if needsReopen {
				r.reopen()
			} else {
				r.m.Unlock()
				time.Sleep(r.sleep)
				r.m.Lock()
			}
		}
	}
}

// needsReopen returns true if file is renamed or truncated, and should be reopened.
func (r *ContinuousFileReader) needsReopen() bool {
	oldFI, err := r.f.Stat()
	if err != nil {
		r.l.Warnf("Failed to stat old file: %s.", err)
		return false
	}
	newFI, err := os.Stat(r.filename) // follows symlink
	if err != nil {
		r.l.Warnf("Failed to stat new file: %s.", err)
		return false
	}
	if !os.SameFile(oldFI, newFI) {
		r.l.Infof("File renamed, resetting.")
		return true
	}

	oldPos, err := r.f.Seek(0, io.SeekCurrent)
	if err != nil {
		r.l.Warnf("Failed to check file position: %s.", err)
		return false
	}
	newSize := newFI.Size()
	if oldPos > newSize {
		r.l.Infof("File truncated (old position %d, new file size %d), resetting.", oldPos, newSize)
		return true
	}

	r.l.Debugf("No need to reset: same file, old position %d, new file size %d.", oldPos, newSize)
	return false
}

// reopen reopens slowlog file.
func (r *ContinuousFileReader) reopen() {
	if err := r.f.Close(); err != nil {
		r.l.Warnf("Failed to close file: %s.", r.f.Name(), err)
	}

	f, err := os.Open(r.filename)
	if err != nil {
		r.l.Warnf("Failed to open file: %s. Closing reader.", r.filename, err)
		r.r = bufio.NewReader(bytes.NewReader(nil))
		r.closed = true
		return
	}

	r.f = f
	r.r = bufio.NewReaderSize(f, readerBufSize)
}

// Close implements Reader interface.
func (r *ContinuousFileReader) Close() error {
	r.m.Lock()
	defer r.m.Unlock()

	err := r.f.Close()
	r.closed = true
	return err
}

// Metrics implements Reader interface.
func (r *ContinuousFileReader) Metrics() *ReaderMetrics {
	r.m.Lock()
	defer r.m.Unlock()

	var m ReaderMetrics

	fi, err := r.f.Stat()
	if err != nil {
		r.l.Warnf("Failed to stat file: %s.", err)
		return nil
	}
	m.InputSize = fi.Size()

	pos, err := r.f.Seek(0, io.SeekCurrent)
	if err != nil {
		r.l.Warnf("Failed to check file position: %s.", err)
		return nil
	}
	m.InputPos = pos

	return &m
}

// check interfaces
var (
	_ Reader = (*ContinuousFileReader)(nil)
)
