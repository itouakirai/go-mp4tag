package mp4tag

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

func makeBox(boxType string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	box := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(box[0:4], uint32(8+len(body)))
	copy(box[4:8], boxType)
	return append(box, body...)
}

// makeTaggableMP4 builds ftyp, moov (udta/meta/ilst and one stco track),
// and an mdat whose header is supplied by the caller.
func makeTaggableMP4(mdatHeader []byte, mdatPayload []byte) ([]byte, int) {
	ftyp := makeBox("ftyp", []byte("isom"), make([]byte, 4), []byte("isom"))
	meta := makeBox("meta", make([]byte, 4), makeBox("ilst"))
	udta := makeBox("udta", meta)

	// stco offset is fixed up below once the moov size is known.
	stco := makeStco([]uint32{0})
	stbl := makeBox("stbl", stco)
	trak := makeBox("trak", makeBox("mdia", makeBox("minf", stbl)))
	moov := makeBox("moov", trak, udta)

	data := append(append([]byte{}, ftyp...), moov...)
	payloadOffset := len(data) + len(mdatHeader)
	stcoAt := bytes.Index(data, []byte("stco")) - 4
	binary.BigEndian.PutUint32(data[stcoAt+16:stcoAt+20], uint32(payloadOffset))

	data = append(data, mdatHeader...)
	data = append(data, mdatPayload...)
	return data, stcoAt
}

func mdatHeader32(size uint32) []byte {
	h := make([]byte, 8)
	binary.BigEndian.PutUint32(h[0:4], size)
	copy(h[4:8], "mdat")
	return h
}

func mdatHeaderLarge(size uint64) []byte {
	h := make([]byte, 16)
	binary.BigEndian.PutUint32(h[0:4], 1)
	copy(h[4:8], "mdat")
	binary.BigEndian.PutUint64(h[8:16], size)
	return h
}

func readStcoAt(t *testing.T, path string) uint32 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(data, []byte("stco")) - 4
	return binary.BigEndian.Uint32(data[at+16 : at+20])
}

func payloadOffsetIn(t *testing.T, path string, payload []byte) uint32 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(data, payload)
	if at < 0 {
		t.Fatal("mdat payload not found")
	}
	return uint32(at)
}

func TestWriteTagsWithMdatSizeForms(t *testing.T) {
	payload := []byte("SAMPLE-DATA-0123456789")
	cases := []struct {
		name   string
		header []byte
	}{
		{"32-bit", mdatHeader32(uint32(8 + len(payload)))},
		// Sizes >= 2 GiB are valid unsigned 32-bit sizes. The fixture is
		// truncated; parsing never reads mdat contents.
		{"32-bit above 2GiB", mdatHeader32(3 << 30)},
		{"64-bit largesize", mdatHeaderLarge(uint64(16 + len(payload)))},
		{"64-bit largesize above 4GiB", mdatHeaderLarge(5 << 30)},
		{"size zero extends to end of file", mdatHeader32(0)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := makeTaggableMP4(tc.header, payload)
			path := writeFixtureFile(t, "in.mp4", data)

			m, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			err = m.Write(&MP4Tags{Title: "Tagged", Artist: "Artist"}, nil)
			if err != nil {
				_ = m.Close()
				t.Fatalf("Write: %v", err)
			}
			tags, err := m.Read()
			_ = m.Close()
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if tags.Title != "Tagged" || tags.Artist != "Artist" {
				t.Fatalf("tags = %q/%q, want Tagged/Artist", tags.Title, tags.Artist)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(got, append(append([]byte{}, tc.header...), payload...)) {
				t.Fatal("mdat header and payload were not preserved")
			}
			if stco, want := readStcoAt(t, path), payloadOffsetIn(t, path, payload); stco != want {
				t.Fatalf("stco offset = %d, want %d", stco, want)
			}
		})
	}
}

func TestReadRejectsInvalidBoxSize(t *testing.T) {
	// A size below the header length used to recurse forever (size 0 at
	// the top level) or walk backwards; it must now fail cleanly.
	data, _ := makeTaggableMP4(mdatHeader32(4), nil)
	path := writeFixtureFile(t, "bad.mp4", data)

	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, err = m.Read()
	if err == nil || !strings.Contains(err.Error(), "invalid size") {
		t.Fatalf("Read error = %v, want invalid size", err)
	}
}
