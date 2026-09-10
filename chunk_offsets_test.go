package mp4tag

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeStco(offsets []uint32) []byte {
	box := make([]byte, 16+4*len(offsets))
	binary.BigEndian.PutUint32(box[0:4], uint32(len(box)))
	copy(box[4:8], "stco")
	binary.BigEndian.PutUint32(box[12:16], uint32(len(offsets)))
	for i, offset := range offsets {
		binary.BigEndian.PutUint32(box[16+4*i:20+4*i], offset)
	}
	return box
}

func makeCo64(offsets []uint64) []byte {
	box := make([]byte, 16+8*len(offsets))
	binary.BigEndian.PutUint32(box[0:4], uint32(len(box)))
	copy(box[4:8], "co64")
	binary.BigEndian.PutUint32(box[12:16], uint32(len(offsets)))
	for i, offset := range offsets {
		binary.BigEndian.PutUint64(box[16+8*i:24+8*i], offset)
	}
	return box
}

func makeOffsetFixture(t *testing.T, boxes ...[]byte) ([]byte, []int64, []int64) {
	t.Helper()
	var data []byte
	offsets := make([]int64, 0, len(boxes))
	sizes := make([]int64, 0, len(boxes))
	for _, box := range boxes {
		offsets = append(offsets, int64(len(data)))
		sizes = append(sizes, int64(len(box)))
		data = append(data, box...)
	}
	return data, offsets, sizes
}

func writeFixtureFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func offsetBoxes(offsets, sizes []int64, path string) []*MP4Box {
	boxes := make([]*MP4Box, 0, len(offsets))
	for i, offset := range offsets {
		boxes = append(boxes, &MP4Box{
			StartOffset: offset,
			EndOffset:   offset + sizes[i],
			BoxSize:     sizes[i],
			Path:        path,
		})
	}
	return boxes
}

func TestUpdateChunkOffsetsUpdatesAllTracks(t *testing.T) {
	data, offsets, sizes := makeOffsetFixture(t,
		makeStco([]uint32{1000, 2000}),
		makeStco([]uint32{3000}),
		makeCo64([]uint64{6 << 30, 7 << 30}),
	)
	srcPath := writeFixtureFile(t, "src.mp4", data)
	outPath := writeFixtureFile(t, "out.mp4", data)

	src, err := os.Open(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	out, err := os.OpenFile(outPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	boxes := MP4Boxes{Boxes: append(
		offsetBoxes(offsets[:2], sizes[:2], "moov.trak.mdia.minf.stbl.stco"),
		offsetBoxes(offsets[2:], sizes[2:], "moov.trak.mdia.minf.stbl.co64")...,
	)}
	mp4 := MP4{f: src, path: srcPath, size: int64(len(data))}
	if err := mp4.updateChunkOffsets(out, boxes, 100, 80); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	assertUint32sAt(t, got, int(offsets[0]), []uint32{980, 1980})
	assertUint32sAt(t, got, int(offsets[1]), []uint32{2980})
	assertUint64sAt(t, got, int(offsets[2]), []uint64{6<<30 - 20, 7<<30 - 20})
}

func TestUpdateChunkOffsetsRejectsUnderflow(t *testing.T) {
	data, offsets, sizes := makeOffsetFixture(t, makeStco([]uint32{5}))
	srcPath := writeFixtureFile(t, "src.mp4", data)
	outPath := writeFixtureFile(t, "out.mp4", data)

	src, err := os.Open(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	out, err := os.OpenFile(outPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	boxes := MP4Boxes{Boxes: offsetBoxes(offsets, sizes, "moov.trak.mdia.minf.stbl.stco")}
	mp4 := MP4{f: src, path: srcPath, size: int64(len(data))}
	err = mp4.updateChunkOffsets(out, boxes, 10, 0)
	if err == nil || !strings.Contains(err.Error(), "smaller than adjustment") {
		t.Fatalf("expected underflow error, got %v", err)
	}
}

func TestUpdateChunkOffsetsRejectsStcoOverflow(t *testing.T) {
	data, offsets, sizes := makeOffsetFixture(t, makeStco([]uint32{^uint32(0)}))
	srcPath := writeFixtureFile(t, "src.mp4", data)
	outPath := writeFixtureFile(t, "out.mp4", data)

	src, err := os.Open(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	out, err := os.OpenFile(outPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	boxes := MP4Boxes{Boxes: offsetBoxes(offsets, sizes, "moov.trak.mdia.minf.stbl.stco")}
	mp4 := MP4{f: src, path: srcPath, size: int64(len(data))}
	err = mp4.updateChunkOffsets(out, boxes, 0, 1)
	if err == nil || !strings.Contains(err.Error(), "32-bit stco range") {
		t.Fatalf("expected stco overflow error, got %v", err)
	}
}

func TestCheckBoxesAllowsCo64WithoutStco(t *testing.T) {
	boxes := MP4Boxes{Boxes: []*MP4Box{
		{Path: "moov"},
		{Path: "mdat"},
		{Path: "moov.udta"},
		{Path: "moov.udta.meta"},
		{Path: "moov.trak.mdia.minf.stbl.co64"},
	}}
	if err := checkBoxes(boxes); err != nil {
		t.Fatal(err)
	}
}

func TestCheckBoxesRejectsMissingOffsetTable(t *testing.T) {
	boxes := MP4Boxes{Boxes: []*MP4Box{
		{Path: "moov"},
		{Path: "mdat"},
		{Path: "moov.udta"},
		{Path: "moov.udta.meta"},
	}}
	err := checkBoxes(boxes)
	if err == nil || !strings.Contains(err.Error(), "stco or co64") {
		t.Fatalf("expected missing offset table error, got %v", err)
	}
}

func assertUint32sAt(t *testing.T, data []byte, boxOffset int, want []uint32) {
	t.Helper()
	count := int(binary.BigEndian.Uint32(data[boxOffset+12 : boxOffset+16]))
	if count != len(want) {
		t.Fatalf("entry count = %d, want %d", count, len(want))
	}
	got := make([]uint32, count)
	for i := range got {
		got[i] = binary.BigEndian.Uint32(data[boxOffset+16+4*i : boxOffset+20+4*i])
	}
	if !equalUint32s(got, want) {
		t.Fatalf("offsets = %v, want %v", got, want)
	}
}

func assertUint64sAt(t *testing.T, data []byte, boxOffset int, want []uint64) {
	t.Helper()
	count := int(binary.BigEndian.Uint32(data[boxOffset+12 : boxOffset+16]))
	if count != len(want) {
		t.Fatalf("entry count = %d, want %d", count, len(want))
	}
	got := make([]uint64, count)
	for i := range got {
		got[i] = binary.BigEndian.Uint64(data[boxOffset+16+8*i : boxOffset+24+8*i])
	}
	if !equalUint64s(got, want) {
		t.Fatalf("offsets = %v, want %v", got, want)
	}
}

func equalUint32s(a, b []uint32) bool {
	return bytes.Equal(uint32Bytes(a), uint32Bytes(b))
}

func uint32Bytes(values []uint32) []byte {
	out := make([]byte, 4*len(values))
	for i, value := range values {
		binary.BigEndian.PutUint32(out[4*i:4*i+4], value)
	}
	return out
}

func equalUint64s(a, b []uint64) bool {
	return bytes.Equal(uint64Bytes(a), uint64Bytes(b))
}

func uint64Bytes(values []uint64) []byte {
	out := make([]byte, 8*len(values))
	for i, value := range values {
		binary.BigEndian.PutUint64(out[8*i:8*i+8], value)
	}
	return out
}
