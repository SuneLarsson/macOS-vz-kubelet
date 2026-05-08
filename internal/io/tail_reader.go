package io

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/virtual-kubelet/virtual-kubelet/node/api"
)

// TailReader wraps an os.File to provide tail, follow, and limit functionalities.
type TailReader struct {
	ctx        context.Context
	file       *os.File
	opts       api.ContainerLogOpts
	bytesRead  int
	limitBytes int
	follow     bool
	isFinished func() bool
}

// NewTailReader creates a new TailReader for the given file and options.
func NewTailReader(ctx context.Context, f *os.File, opts api.ContainerLogOpts, isFinished func() bool) (*TailReader, error) {
	tr := &TailReader{
		ctx:        ctx,
		file:       f,
		opts:       opts,
		limitBytes: opts.LimitBytes,
		follow:     opts.Follow,
		isFinished: isFinished,
	}

	if opts.Tail > 0 {
		if err := tr.seekToTail(opts.Tail); err != nil {
			return nil, err
		}
	}

	return tr, nil
}

func (tr *TailReader) seekToTail(lines int) error {
	stat, err := tr.file.Stat()
	if err != nil {
		return err
	}

	size := stat.Size()
	if size == 0 {
		return nil
	}

	var offset int64
	var lineCount int
	bufSize := int64(4096)
	buf := make([]byte, bufSize)

	for offset = size; offset > 0 && lineCount <= lines; {
		readSize := bufSize
		if offset < bufSize {
			readSize = offset
		}
		offset -= readSize

		if _, err := tr.file.Seek(offset, io.SeekStart); err != nil {
			return err
		}

		if _, err := tr.file.Read(buf[:readSize]); err != nil {
			return err
		}

		for i := readSize - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				lineCount++
				if lineCount == lines+1 {
					// We found the (N+1)th newline from the end. Seek to right after it.
					finalOffset := offset + int64(i) + 1
					if _, err := tr.file.Seek(finalOffset, io.SeekStart); err != nil {
						return err
					}
					return nil
				}
			}
		}
	}

	// If we didn't find enough lines, just seek to the start.
	_, err = tr.file.Seek(0, io.SeekStart)
	return err
}

func (tr *TailReader) Read(p []byte) (int, error) {
	if tr.ctx.Err() != nil {
		return 0, tr.ctx.Err()
	}

	// Apply limit if set
	if tr.limitBytes > 0 && tr.bytesRead >= tr.limitBytes {
		return 0, io.EOF
	}

	var buf []byte
	if tr.limitBytes > 0 {
		remaining := tr.limitBytes - tr.bytesRead
		if len(p) > remaining {
			buf = p[:remaining]
		} else {
			buf = p
		}
	} else {
		buf = p
	}

	for {
		if tr.ctx.Err() != nil {
			return 0, tr.ctx.Err()
		}

		n, err := tr.file.Read(buf)
		if n > 0 {
			tr.bytesRead += n
			if err == io.EOF && tr.follow {
				if tr.isFinished != nil && tr.isFinished() {
					return n, io.EOF
				}
				return n, nil
			}
			return n, err
		}

		if err != nil {
			if err == io.EOF && tr.follow {
				if tr.isFinished != nil && tr.isFinished() {
					return n, io.EOF
				}
				time.Sleep(200 * time.Millisecond) // Poll interval
				continue
			}
			return n, err
		}

		if tr.follow {
			if tr.isFinished != nil && tr.isFinished() {
				return 0, io.EOF
			}
			time.Sleep(200 * time.Millisecond)
		} else {
			return 0, io.EOF
		}
	}
}

func (tr *TailReader) Close() error {
	return tr.file.Close()
}
