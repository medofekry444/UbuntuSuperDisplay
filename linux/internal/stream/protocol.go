// Package stream transports length-prefixed Annex B H.264 access units.
package stream

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const Magic = "USDS0001"
const MaxFrame = 8 * 1024 * 1024

type Header struct{ Width, Height, FPS uint32 }

func WriteHeader(w io.Writer, h Header) error {
	data := make([]byte, 20)
	copy(data, Magic)
	binary.BigEndian.PutUint32(data[8:], h.Width)
	binary.BigEndian.PutUint32(data[12:], h.Height)
	binary.BigEndian.PutUint32(data[16:], h.FPS)
	return writeAll(w, data)
}

func WriteFrame(w io.Writer, frame []byte) error {
	if len(frame) == 0 || len(frame) > MaxFrame {
		return fmt.Errorf("invalid H.264 access unit length: %d", len(frame))
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(frame)))
	if err := writeAll(w, length[:]); err != nil {
		return err
	}
	return writeAll(w, frame)
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// GStreamer's multipartmux preserves access-unit boundaries with explicit
// Content-Length. Emit immediately, without waiting for another desktop frame.
func EncodedFrames(r io.Reader, emit func([]byte) error) error {
	reader := bufio.NewReaderSize(r, 4096)
	line := func() (string, error) {
		data, err := reader.ReadSlice('\n')
		if err == io.EOF && len(data) != 0 {
			return "", io.ErrUnexpectedEOF
		}
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	for {
		boundary, err := line()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if boundary == "" {
			continue
		}
		if boundary == "--usdframe--" {
			return nil
		}
		if boundary != "--usdframe" {
			return errors.New("invalid encoder multipart boundary")
		}
		size := -1
		typeOK := false
		for n := 0; ; n++ {
			if n >= 16 {
				return errors.New("encoder multipart headers exceed limit")
			}
			header, err := line()
			if err != nil {
				return err
			}
			if header == "" {
				break
			}
			key, value, ok := strings.Cut(header, ":")
			if !ok {
				return errors.New("invalid encoder header")
			}
			switch strings.ToLower(key) {
			case "content-length":
				if size != -1 {
					return errors.New("duplicate encoder content length")
				}
				size, err = strconv.Atoi(strings.TrimSpace(value))
				if err != nil {
					return err
				}
			case "content-type":
				typeOK = strings.TrimSpace(value) == "video/x-h264"
			}
		}
		if !typeOK || size < 1 || size > MaxFrame {
			return errors.New("invalid encoder frame type or size")
		}
		frame := make([]byte, size)
		if _, err := io.ReadFull(reader, frame); err != nil {
			return err
		}
		if err := emit(frame); err != nil {
			return err
		}
	}
}
