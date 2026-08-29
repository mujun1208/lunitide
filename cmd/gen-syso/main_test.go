package main

import (
	"encoding/binary"
	"testing"
)

func TestBuildRsrcSectionHasTwoTypesAndDirectoryBits(t *testing.T) {
	entries := []iconDirEntry{{Width: 16, Height: 16, Planes: 1, BitCount: 32, BytesInRes: 4}}
	images := [][]byte{{0x89, 0x50, 0x4e, 0x47}}
	group := buildGroupIconData(entries)
	rsrc, relocs := buildRsrcSection(entries, images, group)
	if len(relocs) != 2 {
		t.Fatalf("relocs = %d want 2 (group + 1 icon)", len(relocs))
	}
	named := binary.LittleEndian.Uint16(rsrc[12:14])
	ids := binary.LittleEndian.Uint16(rsrc[14:16])
	if named != 0 || ids != 2 {
		t.Fatalf("root entries named=%d ids=%d want 0/2", named, ids)
	}
	type0 := binary.LittleEndian.Uint32(rsrc[16:20])
	off0 := binary.LittleEndian.Uint32(rsrc[20:24])
	type1 := binary.LittleEndian.Uint32(rsrc[24:28])
	off1 := binary.LittleEndian.Uint32(rsrc[28:32])
	if type0 != rtIcon || type1 != rtGroupIcon {
		t.Fatalf("root types %d,%d want %d then %d", type0, type1, rtIcon, rtGroupIcon)
	}
	if off0&resourceDataIsDirectory == 0 || off1&resourceDataIsDirectory == 0 {
		t.Fatalf("root type offsets must set IMAGE_RESOURCE_DATA_IS_DIRECTORY: %#x %#x", off0, off1)
	}
	for _, off := range relocs {
		if off >= uint32(len(rsrc)) {
			t.Fatalf("reloc %#x past section", off)
		}
		if binary.LittleEndian.Uint32(rsrc[off:])&resourceDataIsDirectory != 0 {
			t.Fatalf("data entry at %#x still has directory bit", off)
		}
	}
}

func TestBuildGroupIconDataIDsAreOneBased(t *testing.T) {
	entries := []iconDirEntry{
		{Width: 16, Height: 16, Planes: 1, BitCount: 32, BytesInRes: 8},
		{Width: 32, Height: 32, Planes: 1, BitCount: 32, BytesInRes: 8},
	}
	data := buildGroupIconData(entries)
	if binary.LittleEndian.Uint16(data[4:6]) != 2 {
		t.Fatal("count")
	}
	id0 := binary.LittleEndian.Uint16(data[6+12 : 6+14])
	id1 := binary.LittleEndian.Uint16(data[6+14+12 : 6+14+14])
	if id0 != 1 || id1 != 2 {
		t.Fatalf("ids %d,%d", id0, id1)
	}
}

func TestBuildSysoAMD64UsesAddr32NB(t *testing.T) {
	entries := []iconDirEntry{{Width: 16, Height: 16, Planes: 1, BitCount: 32, BytesInRes: 4}}
	images := [][]byte{{0x89, 0x50, 0x4e, 0x47}}
	syso, err := buildSyso(entries, images, machineAMD64, relAMD64Addr32NB)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(syso[0:2]) != machineAMD64 {
		t.Fatalf("machine %x", binary.LittleEndian.Uint16(syso[0:2]))
	}
	rawSize := binary.LittleEndian.Uint32(syso[coffHeaderSize+16 : coffHeaderSize+20])
	relocAt := coffHeaderSize + sectionHeaderSize + int(rawSize)
	if relocAt+10 > len(syso) {
		t.Fatalf("syso too short for reloc")
	}
	if got := binary.LittleEndian.Uint16(syso[relocAt+8 : relocAt+10]); got != relAMD64Addr32NB {
		t.Fatalf("reloc type %d want %d", got, relAMD64Addr32NB)
	}
}
