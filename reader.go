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

type Reader struct {
	r        io.Reader
	state    C.struct_inflate_state
	cInBuf   unsafe.Pointer
	cOutBuf  unsafe.Pointer
	goInBuf  []byte
	buffered int
	consumed int
	finished bool

	// Output buffer management
	outData []byte // decompressed data waiting to be read
	outPos  int    // current position in outData

	// Constants
	inChunk      int
	inBufferCap  int
	outBufferCap int

	err error // any error that occurred
}

func NewReader(r io.Reader) (*Reader, error) {
	const inChunk = 64 * 1024
	const inBufferCap = 8 * 1024 * 1024
	const outBufferCap = 1024 * 1024

	reader := &Reader{
		r:            r,
		inChunk:      inChunk,
		inBufferCap:  inBufferCap,
		outBufferCap: outBufferCap,
	}

	// Initialize inflate state
	C.isal_inflate_init(&reader.state)
	reader.state.crc_flag = C.uint32_t(C.ISAL_GZIP)

	// Allocate C buffers
	reader.cInBuf = C.malloc(C.size_t(inBufferCap))
	if reader.cInBuf == nil {
		return nil, errors.New("alloc failed for input buffer")
	}

	reader.cOutBuf = C.malloc(C.size_t(outBufferCap))
	if reader.cOutBuf == nil {
		C.free(reader.cInBuf)
		return nil, errors.New("alloc failed for output buffer")
	}

	reader.goInBuf = unsafe.Slice((*byte)(reader.cInBuf), inBufferCap)
	reader.outData = make([]byte, 0, outBufferCap)

	return reader, nil
}

func (r *Reader) Read(p []byte) (n int, err error) {
	if r.err != nil {
		return 0, r.err
	}

	// If we have data in output buffer, serve it first
	if r.outPos < len(r.outData) {
		n = copy(p, r.outData[r.outPos:])
		r.outPos += n

		// If we've consumed all output data, reset the buffer
		if r.outPos >= len(r.outData) {
			r.outData = r.outData[:0]
			r.outPos = 0
		}
		return n, nil
	}

	// Reset output buffer for new data
	r.outData = r.outData[:0]
	r.outPos = 0

	// Main decompression loop (similar to Decompress1)
	for !r.finished || r.buffered > r.consumed {
		// Compact buffer if needed
		if r.consumed > 0 && r.inBufferCap-r.buffered < r.inChunk {
			r.buffered -= r.consumed
			C.memmove(r.cInBuf, unsafe.Pointer(uintptr(r.cInBuf)+uintptr(r.consumed)), C.size_t(r.buffered))
			r.consumed = 0
		}

		// Read more data if space available
		if r.inBufferCap-r.buffered >= r.inChunk {
			n, err := r.r.Read(r.goInBuf[r.buffered : r.buffered+r.inChunk])
			if n > 0 {
				r.buffered += n
			}
			if err != nil {
				if err == io.EOF {
					r.finished = true
				} else {
					r.err = err
					return 0, err
				}
			}
		}

		// If no data to process and finished, we're done
		if r.consumed == r.buffered {
			if r.finished {
				return 0, io.EOF
			}
			continue
		}

		// Decompress data
		r.state.next_in = (*C.uint8_t)(unsafe.Pointer(uintptr(r.cInBuf) + uintptr(r.consumed)))
		r.state.avail_in = C.uint32_t(r.buffered - r.consumed)
		r.state.next_out = (*C.uint8_t)(r.cOutBuf)
		r.state.avail_out = C.uint32_t(r.outBufferCap)

		ret := C.isal_inflate(&r.state)
		if ret != 0 && ret != C.ISAL_END_INPUT {
			r.err = fmt.Errorf("decompression failed with code: %d", ret)
			return 0, r.err
		}

		// Get produced data
		produced := r.outBufferCap - int(r.state.avail_out)
		if produced > 0 {
			// Copy decompressed data to our output buffer
			goSlice := unsafe.Slice((*byte)(r.cOutBuf), produced)
			r.outData = append(r.outData, goSlice...)
		}

		// Update consumed counter
		r.consumed = r.buffered - int(r.state.avail_in)

		// Handle end of stream
		if ret == C.ISAL_END_INPUT {
			C.isal_inflate_init(&r.state)
			r.state.crc_flag = C.uint32_t(C.ISAL_GZIP)
		}

		// If we have data to return, break and serve it
		if len(r.outData) > 0 {
			break
		}
	}

	// Serve the decompressed data
	if len(r.outData) > 0 {
		n = copy(p, r.outData)
		r.outPos = n

		// If we've consumed all data, reset
		if r.outPos >= len(r.outData) {
			r.outData = r.outData[:0]
			r.outPos = 0
		}
		return n, nil
	}

	// No more data available
	return 0, io.EOF
}

func (r *Reader) Close() error {
	if r.cInBuf != nil {
		C.free(r.cInBuf)
		r.cInBuf = nil
	}
	if r.cOutBuf != nil {
		C.free(r.cOutBuf)
		r.cOutBuf = nil
	}
	r.goInBuf = nil
	r.outData = nil
	return nil
}

func DecompressCopy(reader io.Reader, writer io.Writer) error {
	const inChunk = 64 * 1024
	const inBufferCap = 8 * 1024 * 1024
	const outBufferCap = 1024 * 1024

	// Initialize inflate state
	var state C.struct_inflate_state
	C.isal_inflate_init(&state)
	state.crc_flag = C.uint32_t(C.ISAL_GZIP)

	// Allocate reusable C buffers
	cInBuf := C.malloc(C.size_t(inBufferCap))
	if cInBuf == nil {
		return errors.New("alloc failed for input buffer")
	}
	defer C.free(cInBuf)
	cOutBuf := C.malloc(C.size_t(outBufferCap))
	if cOutBuf == nil {
		return errors.New("alloc failed for output buffer")
	}
	defer C.free(cOutBuf)

	goInBuf := unsafe.Slice((*byte)(cInBuf), inBufferCap)
	var buffered, consumed int
	var finished bool

	for !finished {
		if consumed > 0 && inBufferCap-buffered < inChunk {
			buffered -= consumed
			C.memmove(cInBuf, unsafe.Pointer(uintptr(cInBuf)+uintptr(consumed)), C.size_t(buffered))
			consumed = 0
		}

		// Read more data if we're not at the end of the file.
		if !finished {
			if inBufferCap-buffered >= inChunk {
				n, err := reader.Read(goInBuf[buffered : buffered+inChunk])
				if n > 0 {
					buffered += n
				}
				if err != nil {
					if err == io.EOF {
						finished = true
					} else {
						return err
					}
				}
			}
		}

		if consumed == buffered {
			if finished {
				return nil
			}
			continue
		}

		state.next_in = (*C.uint8_t)(unsafe.Pointer(uintptr(cInBuf) + uintptr(consumed)))
		state.avail_in = C.uint32_t(buffered - consumed)
		state.next_out = (*C.uint8_t)(cOutBuf)
		state.avail_out = C.uint32_t(outBufferCap)

		ret := C.isal_inflate(&state)
		if ret != 0 && ret != C.ISAL_END_INPUT {
			return fmt.Errorf("decompression failed with code: %d", ret)
		}

		produced := outBufferCap - int(state.avail_out)
		if produced > 0 {
			goSlice := unsafe.Slice((*byte)(cOutBuf), produced)
			if _, err := writer.Write(goSlice); err != nil {
				return err
			}
		}

		consumed = buffered - int(state.avail_in)

		if ret == C.ISAL_END_INPUT {
			C.isal_inflate_init(&state)
			state.crc_flag = C.uint32_t(C.ISAL_GZIP)
		}
	}

	return nil
}
