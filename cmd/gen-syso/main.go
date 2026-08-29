// Package main implements a minimal .syso (COFF object) generator that embeds
// a Windows icon resource into a Go binary without requiring gcc or external
// linkers. The output .syso is consumed by the Go internal linker when
// CGO_ENABLED=0, placing the icon in the PE resource section so Explorer,
// Taskbar, and Alt-Tab display the branded icon before the window is created.
//
// Usage:
//
//	go run ./cmd/gen-syso -ico resources/lunitide-icon.ico -out cmd/desktop/lunitide.syso
//
// The generated .syso is placed next to cmd/desktop/main.go and is automatically
// linked by `go build ./cmd/desktop`.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
)

// COFF machine types.
const (
	machineI386  = 0x014c // IMAGE_FILE_MACHINE_I386
	machineAMD64 = 0x8664 // IMAGE_FILE_MACHINE_AMD64 — required by go build GOARCH=amd64
)

// COFF section characteristics.
const (
	sectionRsrc = 0x40000040 // IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ
)

// COFF symbol storage classes.
const (
	symClassStatic = 3 // IMAGE_SYM_CLASS_STATIC
)

// COFF symbol types.
const (
	symTypeNull = 0
)

// Relocation types.
const (
	relI386Dir32NB   = 0x07 // IMAGE_REL_I386_DIR32NB
	relAMD64Addr32NB = 0x02 // IMAGE_REL_AMD64_ADDR32NB
)

// Windows resource type IDs.
const (
	rtIcon      = 3
	rtGroupIcon = 14
)

// Sizes.
const (
	coffHeaderSize    = 20
	sectionHeaderSize = 40
	symbolSize        = 18
	relocSize         = 10 // 4 (VirtualAddress) + 4 (SymbolTableIndex) + 2 (Type)
)

// iconDirEntry is one entry in the .ico file (16 bytes).
type iconDirEntry struct {
	Width      uint8
	Height     uint8
	ColorCount uint8
	Reserved   uint8
	Planes     uint16
	BitCount   uint16
	BytesInRes uint32
	Offset     uint32
}

func main() {
	icoPath := flag.String("ico", "resources/lunitide-icon.ico", "path to .ico file")
	outPath := flag.String("out", "cmd/desktop/lunitide.syso", "path to output .syso")
	machine := flag.String("machine", "amd64", "COFF machine: i386 or amd64")
	flag.Parse()

	machineID, relocType := uint16(machineI386), uint16(relI386Dir32NB)
	if *machine == "amd64" {
		machineID, relocType = uint16(machineAMD64), uint16(relAMD64Addr32NB)
	}

	icoData, err := os.ReadFile(*icoPath)
	if err != nil {
		fatal("reading .ico: %v", err)
	}

	entries, images, err := parseICO(icoData)
	if err != nil {
		fatal("parsing .ico: %v", err)
	}

	syso, err := buildSyso(entries, images, machineID, relocType)
	if err != nil {
		fatal("building .syso: %v", err)
	}

	if err := os.WriteFile(*outPath, syso, 0644); err != nil {
		fatal("writing .syso: %v", err)
	}
	fmt.Printf("wrote %d bytes to %s (%d icon images, machine=%s)\n", len(syso), *outPath, len(entries), *machine)
}

func parseICO(data []byte) ([]iconDirEntry, [][]byte, error) {
	if len(data) < 6 {
		return nil, nil, fmt.Errorf("file too short for icon dir header")
	}
	typ := binary.LittleEndian.Uint16(data[2:4])
	if typ != 1 {
		return nil, nil, fmt.Errorf("not an icon file (type=%d)", typ)
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count == 0 {
		return nil, nil, fmt.Errorf("icon file has 0 entries")
	}
	headerSize := 6 + count*16
	if len(data) < headerSize {
		return nil, nil, fmt.Errorf("file too short for %d icon dir entries", count)
	}
	entries := make([]iconDirEntry, count)
	images := make([][]byte, count)
	for i := 0; i < count; i++ {
		off := 6 + i*16
		e := iconDirEntry{
			Width:      data[off+0],
			Height:     data[off+1],
			ColorCount: data[off+2],
			Reserved:   data[off+3],
			Planes:     binary.LittleEndian.Uint16(data[off+4 : off+6]),
			BitCount:   binary.LittleEndian.Uint16(data[off+6 : off+8]),
			BytesInRes: binary.LittleEndian.Uint32(data[off+8 : off+12]),
			Offset:     binary.LittleEndian.Uint32(data[off+12 : off+16]),
		}
		imgStart := int(e.Offset)
		imgEnd := imgStart + int(e.BytesInRes)
		if imgEnd > len(data) {
			return nil, nil, fmt.Errorf("icon entry %d image out of bounds", i)
		}
		entries[i] = e
		images[i] = data[imgStart:imgEnd]
	}
	return entries, images, nil
}

// buildSyso constructs a COFF object file with a single .rsrc section
// containing the RT_GROUP_ICON and RT_ICON resources.
func buildSyso(entries []iconDirEntry, images [][]byte, machineID uint16, relocType uint16) ([]byte, error) {
	// 1. Build the RT_GROUP_ICON data blob.
	groupData := buildGroupIconData(entries)

	// 2. Build the .rsrc section data (resource directory tree + data).
	rsrc, dataEntryOffsets := buildRsrcSection(entries, images, groupData)

	// 3. Compute relocations: one per data entry (OffsetToData field, 4 bytes at start of each data entry).
	numRelocs := len(dataEntryOffsets)

	// 4. Compute layout: COFF header + section header + section data + relocations + symbol table + string table.
	symTableOffset := uint32(coffHeaderSize + sectionHeaderSize + len(rsrc) + numRelocs*relocSize)
	numSymbols := uint16(1) // just the .rsrc section symbol

	// 5. Assemble the COFF file.
	var buf []byte

	// COFF File Header (20 bytes)
	hdr := make([]byte, coffHeaderSize)
	binary.LittleEndian.PutUint16(hdr[0:2], machineID)
	binary.LittleEndian.PutUint16(hdr[2:4], 1) // 1 section
	binary.LittleEndian.PutUint32(hdr[8:12], symTableOffset)
	binary.LittleEndian.PutUint16(hdr[12:14], numSymbols)
	binary.LittleEndian.PutUint16(hdr[14:16], 0) // no optional header
	binary.LittleEndian.PutUint16(hdr[16:18], 0) // characteristics
	buf = append(buf, hdr...)

	// Section Header (.rsrc, 40 bytes)
	sec := make([]byte, sectionHeaderSize)
	copy(sec[0:8], ".rsrc\x00\x00\x00")
	binary.LittleEndian.PutUint32(sec[8:12], uint32(len(rsrc)))                                   // VirtualSize
	binary.LittleEndian.PutUint32(sec[12:16], 0)                                                  // VirtualAddress (linker sets)
	binary.LittleEndian.PutUint32(sec[16:20], uint32(len(rsrc)))                                  // SizeOfRawData
	binary.LittleEndian.PutUint32(sec[20:24], uint32(coffHeaderSize+sectionHeaderSize))           // PointerToRawData
	binary.LittleEndian.PutUint32(sec[24:28], uint32(coffHeaderSize+sectionHeaderSize+len(rsrc))) // PointerToRelocations
	binary.LittleEndian.PutUint32(sec[28:32], 0)                                                  // PointerToLinenumbers
	binary.LittleEndian.PutUint16(sec[32:34], uint16(numRelocs))                                  // NumberOfRelocations
	binary.LittleEndian.PutUint16(sec[34:36], 0)                                                  // NumberOfLinenumbers
	binary.LittleEndian.PutUint32(sec[36:40], sectionRsrc)                                        // Characteristics
	buf = append(buf, sec...)

	// Section data
	buf = append(buf, rsrc...)

	// Relocations (10 bytes each: VirtualAddress(4) + SymbolTableIndex(4) + Type(2))
	for _, off := range dataEntryOffsets {
		r := make([]byte, relocSize)
		binary.LittleEndian.PutUint32(r[0:4], off) // VirtualAddress = offset within section
		binary.LittleEndian.PutUint32(r[4:8], 0)   // SymbolTableIndex = 0 (.rsrc symbol)
		binary.LittleEndian.PutUint16(r[8:10], relocType)
		buf = append(buf, r...)
	}

	// Symbol table: one symbol for .rsrc section (18 bytes)
	sym := make([]byte, symbolSize)
	copy(sym[0:8], ".rsrc\x00\x00\x00")                    // short name
	binary.LittleEndian.PutUint32(sym[8:12], 0)            // Value
	binary.LittleEndian.PutUint16(sym[12:14], 1)           // SectionNumber (1-based)
	binary.LittleEndian.PutUint16(sym[14:16], symTypeNull) // Type
	sym[16] = symClassStatic                               // StorageClass = IMAGE_SYM_CLASS_STATIC
	sym[17] = 0                                            // NumberOfAuxSymbols
	buf = append(buf, sym...)

	// String table: 4 bytes (size = 4, meaning empty)
	st := make([]byte, 4)
	binary.LittleEndian.PutUint32(st[0:4], 4)
	buf = append(buf, st...)

	return buf, nil
}

// buildGroupIconData builds the RT_GROUP_ICON resource data.
func buildGroupIconData(entries []iconDirEntry) []byte {
	count := len(entries)
	data := make([]byte, 6+count*14)
	binary.LittleEndian.PutUint16(data[0:2], 0) // reserved
	binary.LittleEndian.PutUint16(data[2:4], 1) // type = icon
	binary.LittleEndian.PutUint16(data[4:6], uint16(count))
	for i, e := range entries {
		off := 6 + i*14
		data[off+0] = e.Width
		data[off+1] = e.Height
		data[off+2] = e.ColorCount
		data[off+3] = e.Reserved
		binary.LittleEndian.PutUint16(data[off+4:off+6], e.Planes)
		binary.LittleEndian.PutUint16(data[off+6:off+8], e.BitCount)
		binary.LittleEndian.PutUint32(data[off+8:off+12], e.BytesInRes)
		binary.LittleEndian.PutUint16(data[off+12:off+14], uint16(i+1)) // resource ID
	}
	return data
}

const resourceDataIsDirectory = uint32(0x80000000)

func dirOffset(off uint32) uint32 { return off | resourceDataIsDirectory }

func align4(n uint32) uint32 { return (n + 3) &^ 3 }

// buildRsrcSection builds the .rsrc section data containing the resource directory
// tree and the resource data blobs. Returns the section bytes and a slice of
// offsets (within the section) to each IMAGE_RESOURCE_DATA_ENTRY's OffsetToData
// field, which need relocations.
//
// Tree (IDs must be sorted at each level):
//
//	Root → RT_ICON(3), RT_GROUP_ICON(14)
//	RT_ICON → ID=1..N → lang 0 → data
//	RT_GROUP_ICON → ID=1 → lang 0 → data
func buildRsrcSection(entries []iconDirEntry, images [][]byte, groupData []byte) ([]byte, []uint32) {
	count := len(entries)
	dirSize := func(n int) uint32 { return uint32(16 + 8*n) }

	off := uint32(0)
	rootOff := off
	off += dirSize(2)
	iconTypeOff := off
	off += dirSize(count)
	groupTypeOff := off
	off += dirSize(1)
	iconNameOffs := make([]uint32, count)
	for i := 0; i < count; i++ {
		iconNameOffs[i] = off
		off += dirSize(1)
	}
	groupNameOff := off
	off += dirSize(1)
	groupDataEntryOff := off
	off += 16
	iconDataEntryOffs := make([]uint32, count)
	for i := 0; i < count; i++ {
		iconDataEntryOffs[i] = off
		off += 16
	}
	off = align4(off)
	groupDataOff := off
	off += uint32(len(groupData))
	off = align4(off)
	iconDataOffs := make([]uint32, count)
	for i := 0; i < count; i++ {
		iconDataOffs[i] = off
		off += uint32(len(images[i]))
		off = align4(off)
	}

	rsrc := make([]byte, off)

	writeResourceDir(rsrc, rootOff, 2)
	binary.LittleEndian.PutUint32(rsrc[rootOff+16+0:], rtIcon)
	binary.LittleEndian.PutUint32(rsrc[rootOff+16+4:], dirOffset(iconTypeOff))
	binary.LittleEndian.PutUint32(rsrc[rootOff+16+8:], rtGroupIcon)
	binary.LittleEndian.PutUint32(rsrc[rootOff+16+12:], dirOffset(groupTypeOff))

	writeResourceDir(rsrc, iconTypeOff, uint16(count))
	for i := 0; i < count; i++ {
		eo := iconTypeOff + 16 + uint32(i)*8
		binary.LittleEndian.PutUint32(rsrc[eo+0:], uint32(i+1))
		binary.LittleEndian.PutUint32(rsrc[eo+4:], dirOffset(iconNameOffs[i]))
	}

	writeResourceDir(rsrc, groupTypeOff, 1)
	binary.LittleEndian.PutUint32(rsrc[groupTypeOff+16+0:], 1)
	binary.LittleEndian.PutUint32(rsrc[groupTypeOff+16+4:], dirOffset(groupNameOff))

	for i := 0; i < count; i++ {
		writeResourceDir(rsrc, iconNameOffs[i], 1)
		binary.LittleEndian.PutUint32(rsrc[iconNameOffs[i]+16+0:], 0) // LANG_NEUTRAL
		binary.LittleEndian.PutUint32(rsrc[iconNameOffs[i]+16+4:], iconDataEntryOffs[i])
	}

	writeResourceDir(rsrc, groupNameOff, 1)
	binary.LittleEndian.PutUint32(rsrc[groupNameOff+16+0:], 0)
	binary.LittleEndian.PutUint32(rsrc[groupNameOff+16+4:], groupDataEntryOff)

	binary.LittleEndian.PutUint32(rsrc[groupDataEntryOff+0:], groupDataOff)
	binary.LittleEndian.PutUint32(rsrc[groupDataEntryOff+4:], uint32(len(groupData)))
	for i := 0; i < count; i++ {
		eo := iconDataEntryOffs[i]
		binary.LittleEndian.PutUint32(rsrc[eo+0:], iconDataOffs[i])
		binary.LittleEndian.PutUint32(rsrc[eo+4:], uint32(len(images[i])))
	}

	copy(rsrc[groupDataOff:], groupData)
	for i := 0; i < count; i++ {
		copy(rsrc[iconDataOffs[i]:], images[i])
	}

	relocOffsets := make([]uint32, 0, 1+count)
	relocOffsets = append(relocOffsets, groupDataEntryOff)
	relocOffsets = append(relocOffsets, iconDataEntryOffs...)
	return rsrc, relocOffsets
}

// writeResourceDir writes an IMAGE_RESOURCE_DIRECTORY header at the given offset.
func writeResourceDir(buf []byte, off uint32, numIDEntries uint16) {
	binary.LittleEndian.PutUint32(buf[off+0:off+4], 0)   // Characteristics
	binary.LittleEndian.PutUint32(buf[off+4:off+8], 0)   // TimeDateStamp
	binary.LittleEndian.PutUint16(buf[off+8:off+10], 0)  // MajorVersion
	binary.LittleEndian.PutUint16(buf[off+10:off+12], 0) // MinorVersion
	binary.LittleEndian.PutUint16(buf[off+12:off+14], 0) // NumberOfNamedEntries
	binary.LittleEndian.PutUint16(buf[off+14:off+16], numIDEntries)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
