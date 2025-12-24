package isal

/*
#cgo LDFLAGS: -lisal
#include <stdlib.h>
#include <string.h>
#include <isa-l/igzip_lib.h>
*/
import "C"
import (
	"errors"
	"fmt"
	"io"
	"unsafe"
)

type Writer struct {
	w        io.Writer
	state    *C.struct_isal_zstream
	cInBuf   unsafe.Pointer
	cOutBuf  unsafe.Pointer
	goInBuf  []byte
	buffered int
	level    int // compression level

	// Output buffer management
	outData []byte // compressed data waiting to be written

	// Constants
	inChunk      int
	inBufferCap  int
	outBufferCap int

	err    error // any error that occurred
	closed bool  // whether the stream has been closed
}

// Compression levels
const (
	DefaultCompression = 1
	NoCompression      = 0
	BestSpeed          = 1
	BestCompression    = 3
)

func NewWriter(w io.Writer) (*Writer, error) {
	return NewWriterLevel(w, DefaultCompression)
}

func NewWriterLevel(w io.Writer, level int) (*Writer, error) {
	const inChunk = 64 * 1024
	const inBufferCap = 8 * 1024 * 1024
	const outBufferCap = 1024 * 1024

	if level < 0 || level > 3 {
		return nil, fmt.Errorf("invalid compression level: %d (must be 0-3)", level)
	}

	writer := &Writer{
		w:            w,
		level:        level,
		inChunk:      inChunk,
		inBufferCap:  inBufferCap,
		outBufferCap: outBufferCap,
	}

	writer.state = (*C.struct_isal_zstream)(C.malloc(C.size_t(unsafe.Sizeof(C.struct_isal_zstream{}))))
	if writer.state == nil {
		return nil, errors.New("alloc failed for state")
	}

	// Initialize deflate state
	C.isal_deflate_init(writer.state)
	writer.state.gzip_flag = C.IGZIP_GZIP
	writer.state.level = C.uint32_t(level)

	// Allocate level_buf according to compression level
	var levelBufSize C.uint
	switch level {
	case 1:
		levelBufSize = C.ISAL_DEF_LVL1_DEFAULT
	case 2:
		levelBufSize = C.ISAL_DEF_LVL2_DEFAULT
	case 3:
		levelBufSize = C.ISAL_DEF_LVL3_DEFAULT
	}

	if level > 0 {
		writer.state.level_buf = (*C.uint8_t)(C.malloc(C.size_t(levelBufSize)))
		if writer.state.level_buf == nil {
			C.free(unsafe.Pointer(writer.state))
			return nil, errors.New("alloc failed for level buffer")
		}
		writer.state.level_buf_size = C.uint32_t(levelBufSize)
	}

	// Allocate C buffers
	writer.cInBuf = C.malloc(C.size_t(inBufferCap))
	if writer.cInBuf == nil {
		if writer.state.level_buf != nil {
			C.free(unsafe.Pointer(writer.state.level_buf))
		}
		C.free(unsafe.Pointer(writer.state))
		return nil, errors.New("alloc failed for input buffer")
	}

	writer.cOutBuf = C.malloc(C.size_t(outBufferCap))
	if writer.cOutBuf == nil {
		C.free(writer.cInBuf)
		if writer.state.level_buf != nil {
			C.free(unsafe.Pointer(writer.state.level_buf))
		}
		C.free(unsafe.Pointer(writer.state))
		return nil, errors.New("alloc failed for output buffer")
	}

	writer.goInBuf = unsafe.Slice((*byte)(writer.cInBuf), inBufferCap)
	writer.outData = make([]byte, 0, outBufferCap)

	return writer, nil
}

func (w *Writer) Write(p []byte) (n int, err error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.closed {
		return 0, errors.New("write on closed writer")
	}

	totalWritten := 0
	remaining := p

	for len(remaining) > 0 {
		// Calculate how much we can buffer
		available := w.inBufferCap - w.buffered
		toWrite := len(remaining)
		if toWrite > available {
			toWrite = available
		}

		// Copy data to buffer
		copy(w.goInBuf[w.buffered:], remaining[:toWrite])
		w.buffered += toWrite
		remaining = remaining[toWrite:]
		totalWritten += toWrite

		// Compress if buffer is full or we've written all input
		if w.buffered >= w.inBufferCap || len(remaining) == 0 {
			if err := w.compress(false); err != nil {
				w.err = err
				return totalWritten, err
			}
		}
	}

	return totalWritten, nil
}

func (w *Writer) compress(flush bool) error {
	if w.err != nil {
		return w.err
	}

	// Set up compression state
	w.state.next_in = (*C.uint8_t)(w.cInBuf)
	w.state.avail_in = C.uint32_t(w.buffered)

	if flush {
		w.state.end_of_stream = 1
	}

	for {
		w.state.next_out = (*C.uint8_t)(w.cOutBuf)
		w.state.avail_out = C.uint32_t(w.outBufferCap)

		ret := C.isal_deflate(w.state)
		if ret != C.ISAL_DECOMP_OK {
			return fmt.Errorf("compression failed with code: %d", ret)
		}

		produced := w.outBufferCap - int(w.state.avail_out)
		if produced > 0 {
			goSlice := unsafe.Slice((*byte)(w.cOutBuf), produced)
			if _, err := w.w.Write(goSlice); err != nil {
				return err
			}
		}

		// If flushing, continue until the stream is finished
		if flush && w.state.internal_state.state != C.ZSTATE_END {
			continue
		}

		break
	}

	// Update buffered counter
	consumed := w.buffered - int(w.state.avail_in)
	if consumed > 0 {
		remaining := w.buffered - consumed
		if remaining > 0 {
			C.memmove(w.cInBuf,
				unsafe.Pointer(uintptr(w.cInBuf)+uintptr(consumed)),
				C.size_t(remaining))
		}
		w.buffered = remaining
	}

	return nil
}

func (w *Writer) Flush() error {
	if w.err != nil {
		return w.err
	}
	if w.closed {
		return errors.New("flush on closed writer")
	}

	return w.compress(false)
}

func (w *Writer) Close() error {
	if w.closed {
		return nil
	}

	// Flush any remaining data with finish flush
	if w.err == nil {
		w.err = w.compress(true)
	}

	// Free C buffers including level_buf
	if w.state != nil {
		if w.state.level_buf != nil {
			C.free(unsafe.Pointer(w.state.level_buf))
			w.state.level_buf = nil
		}
		C.free(unsafe.Pointer(w.state))
		w.state = nil
	}
	if w.cInBuf != nil {
		C.free(w.cInBuf)
		w.cInBuf = nil
	}
	if w.cOutBuf != nil {
		C.free(w.cOutBuf)
		w.cOutBuf = nil
	}
	w.goInBuf = nil
	w.outData = nil
	w.closed = true

	return w.err
}

func CompressCopy(reader io.Reader, writer io.Writer) error {
	return CompressCopyLevel(reader, writer, DefaultCompression)
}

func CompressCopyLevel(reader io.Reader, writer io.Writer, level int) error {
	w, err := NewWriterLevel(writer, level)
	if err != nil {
		return err
	}

	buf := make([]byte, 64*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return w.Close()
}
