// Package compilerdaemon implements the optional out-of-process Dragline
// compiler service. The wire contract is deliberately small, versioned, and
// bounded; clients still validate every returned .wago artifact locally.
package compilerdaemon

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
)

const ProtocolVersion uint32 = 1

var requestMagic = [8]byte{'W', 'A', 'G', 'O', 'D', 'M', 'N', '1'}
var responseMagic = [8]byte{'W', 'A', 'G', 'O', 'D', 'M', 'R', '1'}

const (
	requestHeaderSize  = 72
	responseHeaderSize = 64
	operationCompile   = 1
	responseOK         = 0
	responseError      = 1
)

type requestHeader struct {
	ID            uint64
	OptionsLength uint32
	WasmLength    uint64
	WasmHash      [32]byte
}

func encodeRequestHeader(dst []byte, header requestHeader) {
	copy(dst[:8], requestMagic[:])
	binary.LittleEndian.PutUint32(dst[8:12], ProtocolVersion)
	binary.LittleEndian.PutUint16(dst[12:14], operationCompile)
	binary.LittleEndian.PutUint64(dst[16:24], header.ID)
	binary.LittleEndian.PutUint32(dst[24:28], header.OptionsLength)
	binary.LittleEndian.PutUint64(dst[32:40], header.WasmLength)
	copy(dst[40:72], header.WasmHash[:])
}

func decodeRequestHeader(src []byte) (requestHeader, error) {
	if len(src) != requestHeaderSize || string(src[:8]) != string(requestMagic[:]) {
		return requestHeader{}, fmt.Errorf("compiler daemon: invalid request magic")
	}
	if version := binary.LittleEndian.Uint32(src[8:12]); version != ProtocolVersion {
		return requestHeader{}, fmt.Errorf("compiler daemon: request version %d unsupported", version)
	}
	if operation := binary.LittleEndian.Uint16(src[12:14]); operation != operationCompile || binary.LittleEndian.Uint16(src[14:16]) != 0 || binary.LittleEndian.Uint32(src[28:32]) != 0 {
		return requestHeader{}, fmt.Errorf("compiler daemon: invalid request operation or reserved fields")
	}
	var wasmHash [32]byte
	copy(wasmHash[:], src[40:72])
	return requestHeader{
		ID: binary.LittleEndian.Uint64(src[16:24]), OptionsLength: binary.LittleEndian.Uint32(src[24:28]),
		WasmLength: binary.LittleEndian.Uint64(src[32:40]), WasmHash: wasmHash,
	}, nil
}

type responseHeader struct {
	ID      uint64
	Status  uint32
	Length  uint64
	Payload [32]byte
}

func encodeResponseHeader(dst []byte, header responseHeader) {
	copy(dst[:8], responseMagic[:])
	binary.LittleEndian.PutUint32(dst[8:12], ProtocolVersion)
	binary.LittleEndian.PutUint32(dst[12:16], header.Status)
	binary.LittleEndian.PutUint64(dst[16:24], header.ID)
	binary.LittleEndian.PutUint64(dst[24:32], header.Length)
	copy(dst[32:64], header.Payload[:])
}

func decodeResponseHeader(src []byte) (responseHeader, error) {
	if len(src) != responseHeaderSize || string(src[:8]) != string(responseMagic[:]) {
		return responseHeader{}, fmt.Errorf("compiler daemon: invalid response magic")
	}
	if version := binary.LittleEndian.Uint32(src[8:12]); version != ProtocolVersion {
		return responseHeader{}, fmt.Errorf("compiler daemon: response version %d unsupported", version)
	}
	status := binary.LittleEndian.Uint32(src[12:16])
	if status != responseOK && status != responseError {
		return responseHeader{}, fmt.Errorf("compiler daemon: invalid response status %d", status)
	}
	header := responseHeader{ID: binary.LittleEndian.Uint64(src[16:24]), Status: status, Length: binary.LittleEndian.Uint64(src[24:32])}
	copy(header.Payload[:], src[32:64])
	return header, nil
}

func requestHashMatches(wasm []byte, encoded [32]byte) bool {
	return sha256.Sum256(wasm) == encoded
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) != 0 {
		n, err := w.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
