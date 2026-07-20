package embedded32

import "encoding/binary"

const (
	FirmwareArtifactVersion       = uint32(1)
	FirmwareArtifactHeaderBytes   = uint32(40)
	FirmwareArtifactFunctionBytes = uint32(12)
)

var firmwareArtifactMagic = [4]byte{'W', '2', 'A', 'R'}

// FirmwareArtifact is the transport representation of one compiled image and
// the bounded metadata needed to instantiate and call it. Image aliases the
// encoded artifact on decode; Contexts and Functions use caller-owned storage.
type FirmwareArtifact struct {
	Target         uint32
	ImageAddress   uint32
	ContextAddress uint32
	StartAddress   uint32
	Image          []byte
	Contexts       []uint32
	Functions      []FirmwareTransportFunction
}

func FirmwareArtifactSize(imageBytes, contextCount, functionCount uint32) (uint32, bool) {
	metadata := uint64(FirmwareArtifactHeaderBytes) + uint64(contextCount)*4 + uint64(functionCount)*uint64(FirmwareArtifactFunctionBytes)
	imageOffset := (metadata + 15) &^ uint64(15)
	total := imageOffset + uint64(imageBytes)
	if imageBytes == 0 || contextCount == 0 || total > uint64(^uint32(0)) {
		return 0, false
	}
	return uint32(total), true
}

// EncodeFirmwareArtifact serializes artifact into dst without retaining dst.
func EncodeFirmwareArtifact(dst []byte, artifact FirmwareArtifact) (uint32, error) {
	if uint64(len(artifact.Image)) > uint64(^uint32(0)) || uint64(len(artifact.Contexts)) > uint64(^uint32(0)) || uint64(len(artifact.Functions)) > uint64(^uint32(0)) {
		return 0, ErrTransportCapacity
	}
	size, ok := FirmwareArtifactSize(uint32(len(artifact.Image)), uint32(len(artifact.Contexts)), uint32(len(artifact.Functions)))
	if !ok || len(dst) < int(size) {
		return 0, ErrTransportCapacity
	}
	imageOffset := firmwareArtifactImageOffset(uint32(len(artifact.Contexts)), uint32(len(artifact.Functions)))
	if !firmwareArtifactValid(artifact, imageOffset, size) {
		return 0, ErrTransportFrame
	}
	clear(dst[:size])
	copy(dst[:4], firmwareArtifactMagic[:])
	binary.LittleEndian.PutUint32(dst[4:8], FirmwareArtifactVersion)
	binary.LittleEndian.PutUint32(dst[8:12], artifact.Target)
	binary.LittleEndian.PutUint32(dst[12:16], artifact.ImageAddress)
	binary.LittleEndian.PutUint32(dst[16:20], uint32(len(artifact.Image)))
	binary.LittleEndian.PutUint32(dst[20:24], artifact.ContextAddress)
	binary.LittleEndian.PutUint32(dst[24:28], artifact.StartAddress)
	binary.LittleEndian.PutUint32(dst[28:32], uint32(len(artifact.Contexts)))
	binary.LittleEndian.PutUint32(dst[32:36], uint32(len(artifact.Functions)))
	binary.LittleEndian.PutUint32(dst[36:40], imageOffset)
	offset := FirmwareArtifactHeaderBytes
	for _, context := range artifact.Contexts {
		binary.LittleEndian.PutUint32(dst[offset:offset+4], context)
		offset += 4
	}
	for _, function := range artifact.Functions {
		binary.LittleEndian.PutUint32(dst[offset:offset+4], function.Address)
		binary.LittleEndian.PutUint32(dst[offset+4:offset+8], function.Context)
		binary.LittleEndian.PutUint16(dst[offset+8:offset+10], function.ParamSlots)
		binary.LittleEndian.PutUint16(dst[offset+10:offset+12], function.ResultSlots)
		offset += FirmwareArtifactFunctionBytes
	}
	copy(dst[imageOffset:size], artifact.Image)
	return size, nil
}

// DecodeFirmwareArtifact strictly decodes src into caller-owned metadata
// storage. It performs no allocation and rejects non-canonical padding.
func DecodeFirmwareArtifact(src []byte, contextStorage []uint32, functionStorage []FirmwareTransportFunction) (FirmwareArtifact, error) {
	if len(src) < int(FirmwareArtifactHeaderBytes) || src[0] != firmwareArtifactMagic[0] || src[1] != firmwareArtifactMagic[1] ||
		src[2] != firmwareArtifactMagic[2] || src[3] != firmwareArtifactMagic[3] ||
		binary.LittleEndian.Uint32(src[4:8]) != FirmwareArtifactVersion {
		return FirmwareArtifact{}, ErrTransportFrame
	}
	imageBytes := binary.LittleEndian.Uint32(src[16:20])
	contextCount := binary.LittleEndian.Uint32(src[28:32])
	functionCount := binary.LittleEndian.Uint32(src[32:36])
	imageOffset := binary.LittleEndian.Uint32(src[36:40])
	size, ok := FirmwareArtifactSize(imageBytes, contextCount, functionCount)
	if !ok || uint64(size) != uint64(len(src)) || imageOffset != firmwareArtifactImageOffset(contextCount, functionCount) ||
		uint64(contextCount) > uint64(len(contextStorage)) || uint64(functionCount) > uint64(len(functionStorage)) {
		return FirmwareArtifact{}, ErrTransportFrame
	}
	metadataEnd := FirmwareArtifactHeaderBytes + contextCount*4 + functionCount*FirmwareArtifactFunctionBytes
	for _, value := range src[metadataEnd:imageOffset] {
		if value != 0 {
			return FirmwareArtifact{}, ErrTransportFrame
		}
	}
	artifact := FirmwareArtifact{
		Target:         binary.LittleEndian.Uint32(src[8:12]),
		ImageAddress:   binary.LittleEndian.Uint32(src[12:16]),
		ContextAddress: binary.LittleEndian.Uint32(src[20:24]),
		StartAddress:   binary.LittleEndian.Uint32(src[24:28]),
		Image:          src[imageOffset:size],
		Contexts:       contextStorage[:contextCount],
		Functions:      functionStorage[:functionCount],
	}
	offset := FirmwareArtifactHeaderBytes
	for index := range artifact.Contexts {
		artifact.Contexts[index] = binary.LittleEndian.Uint32(src[offset : offset+4])
		offset += 4
	}
	for index := range artifact.Functions {
		artifact.Functions[index] = FirmwareTransportFunction{
			Address:     binary.LittleEndian.Uint32(src[offset : offset+4]),
			Context:     binary.LittleEndian.Uint32(src[offset+4 : offset+8]),
			ParamSlots:  binary.LittleEndian.Uint16(src[offset+8 : offset+10]),
			ResultSlots: binary.LittleEndian.Uint16(src[offset+10 : offset+12]),
		}
		offset += FirmwareArtifactFunctionBytes
	}
	if !firmwareArtifactValid(artifact, imageOffset, size) {
		return FirmwareArtifact{}, ErrTransportFrame
	}
	return artifact, nil
}

func firmwareArtifactImageOffset(contextCount, functionCount uint32) uint32 {
	metadata := uint64(FirmwareArtifactHeaderBytes) + uint64(contextCount)*4 + uint64(functionCount)*uint64(FirmwareArtifactFunctionBytes)
	return uint32((metadata + 15) &^ uint64(15))
}

func firmwareArtifactValid(artifact FirmwareArtifact, imageOffset, totalSize uint32) bool {
	if artifact.Target != TransportTargetArm32 && artifact.Target != TransportTargetRISCV32 ||
		artifact.ImageAddress == 0 || artifact.ContextAddress == 0 || len(artifact.Image) == 0 || len(artifact.Contexts) == 0 ||
		uint64(imageOffset)+uint64(len(artifact.Image)) != uint64(totalSize) {
		return false
	}
	contains := func(address, length uint32) bool {
		_, ok := firmwareRangeOffset(artifact.ImageAddress, uint32(len(artifact.Image)), address, length)
		return ok
	}
	callable := func(address uint32) bool {
		if address == 0 {
			return false
		}
		if artifact.Target == TransportTargetArm32 {
			if address&1 == 0 {
				return false
			}
			address &^= 1
		} else if address&1 != 0 {
			return false
		}
		return contains(address, 1)
	}
	if artifact.Contexts[0] != artifact.ContextAddress || !contains(artifact.ContextAddress, ContextABISize) ||
		artifact.StartAddress != 0 && !callable(artifact.StartAddress) {
		return false
	}
	knownContext := func(address uint32) bool {
		for _, context := range artifact.Contexts {
			if context == address {
				return true
			}
		}
		return false
	}
	for _, context := range artifact.Contexts {
		if !contains(context, ContextABISize) {
			return false
		}
	}
	for _, function := range artifact.Functions {
		context := function.Context
		if context == 0 {
			context = artifact.ContextAddress
		}
		if !callable(function.Address) || !knownContext(context) {
			return false
		}
	}
	return true
}
