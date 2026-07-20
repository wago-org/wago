// Command pico2-probe verifies the framed WAGO transport exposed by Pico 2
// firmware and can transactionally upload one host-compiled target image.
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func main() {
	port := flag.String("port", "", "serial device (default: first /dev/ttyACM*)")
	timeout := flag.Duration("timeout", 30*time.Second, "overall response timeout")
	uploadPath := flag.String("upload", "", "image file to upload and checksum-commit")
	callExport := flag.Int("call", -1, "instantiate, start, and call this export ordinal")
	callArgs := flag.String("args", "", "comma-separated raw 32-bit parameter slots")
	callResults := flag.Uint("results", 0, "number of raw 32-bit result slots")
	reset := flag.Bool("reset", false, "reset the current image before optional instantiation and call")
	flag.Parse()
	if *callExport < -1 || *callExport >= 0 && uint64(*callExport) > uint64(^uint32(0)) || uint64(*callResults) > uint64(^uint32(0)) {
		fatalf("invalid call shape")
	}
	if *port == "" {
		matches, err := filepath.Glob("/dev/ttyACM*")
		if err != nil || len(matches) == 0 {
			fatalf("no Pico 2 serial device found; pass -port")
		}
		*port = matches[0]
	}
	if err := configureSerial(*port); err != nil {
		fatalf("configure %s: %v", *port, err)
	}
	serial, err := os.OpenFile(*port, os.O_RDWR, 0)
	if err != nil {
		fatalf("open %s: %v", *port, err)
	}
	defer serial.Close()
	timer := time.AfterFunc(*timeout, func() { _ = serial.Close() })
	defer timer.Stop()

	client := transportClient{stream: serial, maximumPayload: 4096}
	frame, err := client.exchange(embedded32.TransportHello, nil)
	if err != nil {
		fatalf("hello: %v", err)
	}
	hello, err := embedded32.DecodeTransportHello(frame.Payload)
	if err != nil {
		fatalf("decode hello payload: %v", err)
	}
	targetName := ""
	switch hello.Target {
	case embedded32.TransportTargetArm32:
		targetName = "arm32"
	case embedded32.TransportTargetRISCV32:
		targetName = "riscv32"
	default:
		fatalf("unsupported target = %d", hello.Target)
	}
	client.maximumPayload = hello.MaximumPayload
	fmt.Printf("Pico 2 hello: target=%s context_abi=%d call_abi=%d max_payload=%d port=%s\n",
		targetName, hello.ContextABIBytes, hello.CallABIBytes, hello.MaximumPayload, *port)
	frame, err = client.exchange(embedded32.TransportUploadStatus, nil)
	if err != nil {
		fatalf("upload status: %v", err)
	}
	status, err := embedded32.DecodeTransportUploadStatus(frame.Payload)
	if err != nil {
		fatalf("decode upload status: %v", err)
	}
	fmt.Printf("Pico 2 upload arena: base=%#08x capacity=%d max_chunk=%d state=%d image_bytes=%d checksum=%#08x\n",
		status.BaseAddress, status.Capacity, status.MaximumChunk, status.State, status.ImageBytes, status.ImageChecksum)
	if *uploadPath != "" {
		if hello.MaximumPayload <= embedded32.TransportUploadChunkHeader {
			fatalf("maximum payload %d cannot carry an upload chunk", hello.MaximumPayload)
		}
		image, err := os.ReadFile(*uploadPath)
		if err != nil {
			fatalf("read upload image: %v", err)
		}
		if len(image) == 0 || uint64(len(image)) > uint64(status.Capacity) {
			fatalf("upload image bytes = %d, capacity = %d", len(image), status.Capacity)
		}
		checksum := embedded32.TransportChecksum(image)
		beginPayload := make([]byte, embedded32.TransportUploadBeginBytes)
		if err := embedded32.EncodeTransportUploadBegin(beginPayload, embedded32.TransportUploadBeginRequest{
			ImageBytes: uint32(len(image)), ImageChecksum: checksum,
		}); err != nil {
			fatalf("encode upload begin: %v", err)
		}
		if _, err := client.exchange(embedded32.TransportUploadBegin, beginPayload); err != nil {
			fatalf("upload begin: %v", err)
		}
		chunkBytes := min(status.MaximumChunk, hello.MaximumPayload-embedded32.TransportUploadChunkHeader)
		chunkPayload := make([]byte, embedded32.TransportUploadChunkHeader+chunkBytes)
		for offset := uint32(0); offset < uint32(len(image)); {
			end := min(offset+chunkBytes, uint32(len(image)))
			n, err := embedded32.EncodeTransportUploadChunk(chunkPayload, embedded32.TransportUploadChunkRequest{
				Offset: offset,
				Bytes:  image[offset:end],
			})
			if err != nil {
				fatalf("encode upload chunk at %d: %v", offset, err)
			}
			if _, err := client.exchange(embedded32.TransportUploadChunk, chunkPayload[:n]); err != nil {
				fatalf("upload chunk at %d: %v", offset, err)
			}
			offset = end
		}
		if _, err := client.exchange(embedded32.TransportUploadCommit, nil); err != nil {
			fatalf("upload commit: %v", err)
		}
		frame, err = client.exchange(embedded32.TransportUploadStatus, nil)
		if err != nil {
			fatalf("committed upload status: %v", err)
		}
		committed, err := embedded32.DecodeTransportUploadStatus(frame.Payload)
		if err != nil {
			fatalf("decode committed upload status: %v", err)
		}
		if committed.State != embedded32.TransportUploadCommitted || committed.ImageBytes != uint32(len(image)) || committed.ImageChecksum != checksum {
			fatalf("commit mismatch: state=%d bytes=%d checksum=%#x", committed.State, committed.ImageBytes, committed.ImageChecksum)
		}
		fmt.Printf("Pico 2 upload committed: bytes=%d checksum=%#08x\n", len(image), checksum)
	}
	if *reset {
		if _, err := client.exchange(embedded32.TransportReset, nil); err != nil {
			fatalf("reset: %v", err)
		}
		fmt.Println("Pico 2 reset: ok")
	}
	if *callExport >= 0 {
		parameters, err := parseSlots(*callArgs)
		if err != nil {
			fatalf("parse call arguments: %v", err)
		}
		if _, err := client.exchange(embedded32.TransportInstantiate, nil); err != nil {
			fatalf("instantiate: %v", err)
		}
		if _, err := client.exchange(embedded32.TransportStart, nil); err != nil {
			fatalf("start: %v", err)
		}
		payloadBytes, ok := embedded32.TransportCallRequestBytes(uint32(len(parameters)))
		if !ok || payloadBytes > hello.MaximumPayload || uint64(*callResults)*4 > uint64(hello.MaximumPayload) {
			fatalf("call shape exceeds maximum payload %d", hello.MaximumPayload)
		}
		payload := make([]byte, payloadBytes)
		n, err := embedded32.EncodeTransportCallRequest(payload, embedded32.TransportCallRequest{
			ExportIndex: uint32(*callExport), ParameterSlots: parameters, ResultSlots: uint32(*callResults),
		})
		if err != nil {
			fatalf("encode call: %v", err)
		}
		frame, err := client.exchangeAny(embedded32.TransportCall, payload[:n])
		if err != nil {
			fatalf("call: %v", err)
		}
		if frame.Code != embedded32.TransportCodeOK {
			if trap, ok := frame.Code.Trap(); ok {
				fatalf("call trapped: %d", trap)
			}
			fatalf("call response code=%#x", frame.Code)
		}
		results := make([]uint32, *callResults)
		if _, err := embedded32.DecodeTransportSlots(frame.Payload, results, uint32(*callResults)); err != nil {
			fatalf("decode call results: %v", err)
		}
		fmt.Printf("Pico 2 call: export=%d args=%v results=%v", *callExport, parameters, results)
		if len(results) == 2 {
			fmt.Printf(" result_u64=%d", uint64(results[0])|uint64(results[1])<<32)
		}
		fmt.Println()
	}
}

type transportClient struct {
	stream         io.ReadWriter
	sequence       uint32
	maximumPayload uint32
}

func (c *transportClient) exchange(kind embedded32.TransportKind, payload []byte) (embedded32.TransportFrame, error) {
	frame, err := c.exchangeAny(kind, payload)
	if err != nil {
		return embedded32.TransportFrame{}, err
	}
	if frame.Code != embedded32.TransportCodeOK {
		return embedded32.TransportFrame{}, fmt.Errorf("response code=%#x", frame.Code)
	}
	return frame, nil
}

func (c *transportClient) exchangeAny(kind embedded32.TransportKind, payload []byte) (embedded32.TransportFrame, error) {
	if c.stream == nil || uint64(len(payload)) > uint64(c.maximumPayload) {
		return embedded32.TransportFrame{}, embedded32.ErrTransportCapacity
	}
	c.sequence++
	request := make([]byte, int(embedded32.TransportHeaderBytes)+len(payload))
	n, err := embedded32.EncodeTransportFrame(request, embedded32.TransportFrame{Kind: kind, Sequence: c.sequence, Payload: payload})
	if err != nil {
		return embedded32.TransportFrame{}, err
	}
	if err := writeFull(c.stream, request[:n]); err != nil {
		return embedded32.TransportFrame{}, err
	}
	header := make([]byte, embedded32.TransportHeaderBytes)
	if _, err := io.ReadFull(c.stream, header); err != nil {
		return embedded32.TransportFrame{}, err
	}
	payloadBytes := binary.LittleEndian.Uint32(header[12:16])
	if payloadBytes > c.maximumPayload {
		return embedded32.TransportFrame{}, embedded32.ErrTransportCapacity
	}
	response := make([]byte, embedded32.TransportHeaderBytes+payloadBytes)
	copy(response, header)
	if _, err := io.ReadFull(c.stream, response[embedded32.TransportHeaderBytes:]); err != nil {
		return embedded32.TransportFrame{}, err
	}
	frame, consumed, err := embedded32.DecodeTransportFrame(response, c.maximumPayload)
	if err != nil {
		return embedded32.TransportFrame{}, err
	}
	if consumed != uint32(len(response)) || frame.Kind != kind.Response() || frame.Sequence != c.sequence {
		return embedded32.TransportFrame{}, fmt.Errorf("unexpected response kind=%d sequence=%d", frame.Kind, frame.Sequence)
	}
	return frame, nil
}

func parseSlots(value string) ([]uint32, error) {
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	slots := make([]uint32, len(parts))
	for index, part := range parts {
		parsed, err := strconv.ParseUint(strings.TrimSpace(part), 0, 32)
		if err != nil {
			return nil, err
		}
		slots[index] = uint32(parsed)
	}
	return slots, nil
}

func configureSerial(port string) error {
	var args []string
	switch runtime.GOOS {
	case "linux":
		args = []string{"-F", port, "raw", "-echo", "115200"}
	case "darwin":
		args = []string{"-f", port, "raw", "-echo", "115200"}
	default:
		return errors.New("automatic raw serial configuration is supported only on Linux and macOS")
	}
	output, err := exec.Command("stty", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("stty: %w: %s", err, output)
	}
	return nil
}

func writeFull(dst io.Writer, src []byte) error {
	for len(src) != 0 {
		written, err := dst.Write(src)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(src) {
			return io.ErrShortWrite
		}
		src = src[written:]
	}
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pico2-probe: "+format+"\n", args...)
	os.Exit(1)
}
