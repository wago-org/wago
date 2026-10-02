//go:build windows

package build

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

func TestWindowsBuildOutputNamedStream(t *testing.T) {
	defaultStream := windowsTestStreamInformation("::$DATA")
	if name, named, err := windowsBuildOutputNamedStream(defaultStream); err != nil || named {
		t.Fatalf("default stream = %q, %v, %v", name, named, err)
	}
	namedStream := windowsTestStreamInformation(":Zone.Identifier:$DATA")
	if name, named, err := windowsBuildOutputNamedStream(namedStream); err != nil || !named || name != ":Zone.Identifier:$DATA" {
		t.Fatalf("named stream = %q, %v, %v", name, named, err)
	}
	for _, malformed := range [][]byte{
		make([]byte, 23),
		func() []byte {
			value := windowsTestStreamInformation("::$DATA")
			binary.LittleEndian.PutUint32(value[4:], 3)
			return value
		}(),
		func() []byte {
			value := windowsTestStreamInformation("::$DATA")
			binary.LittleEndian.PutUint32(value, 8)
			return value
		}(),
	} {
		if _, _, err := windowsBuildOutputNamedStream(malformed); err == nil {
			t.Fatalf("malformed stream metadata accepted: %x", malformed)
		}
	}
}

func TestSameWindowsSecurityDescriptor(t *testing.T) {
	low, err := windows.SecurityDescriptorFromString("S:(ML;;NW;;;LW)")
	if err != nil {
		t.Fatal(err)
	}
	lowCopy, err := windows.SecurityDescriptorFromString("S:(ML;;NW;;;LW)")
	if err != nil {
		t.Fatal(err)
	}
	medium, err := windows.SecurityDescriptorFromString("S:(ML;;NW;;;ME)")
	if err != nil {
		t.Fatal(err)
	}
	if !sameWindowsSecurityDescriptor(low, lowCopy) {
		t.Fatal("identical integrity descriptors compared different")
	}
	if sameWindowsSecurityDescriptor(low, medium) {
		t.Fatal("distinct integrity descriptors compared equal")
	}
	if !sameBuildOutputMetadata(
		buildOutputMetadata{accessPolicyDescriptor: low},
		buildOutputMetadata{accessPolicyDescriptor: lowCopy},
	) {
		t.Fatal("identical access-policy descriptors compared different")
	}
	if sameBuildOutputMetadata(
		buildOutputMetadata{accessPolicyDescriptor: low},
		buildOutputMetadata{accessPolicyDescriptor: medium},
	) {
		t.Fatal("distinct access-policy descriptors compared equal")
	}
}

func windowsTestStreamInformation(name string) []byte {
	units := utf16.Encode([]rune(name))
	buffer := make([]byte, 24+len(units)*2)
	binary.LittleEndian.PutUint32(buffer[4:], uint32(len(units)*2))
	for index, unit := range units {
		binary.LittleEndian.PutUint16(buffer[24+index*2:], unit)
	}
	return buffer
}
