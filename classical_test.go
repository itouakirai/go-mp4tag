package mp4tag

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func classicalTags() *MP4Tags {
	return &MP4Tags{
		Title:               "I. Allegro vivace",
		Work:                "Piano Sonata No. 16 in G Major, Op. 31/1",
		Movement:            "I. Allegro vivace",
		MovementIndex:       258,
		MovementCount:       300,
		ShowWorkAndMovement: true,
	}
}

// ilstChild returns the data box payload (type and value) of the named ilst
// child, found by scanning the raw file rather than through Read.
func ilstChild(t *testing.T, path, name string) (dataType uint32, value []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(data, []byte(name))
	if at < 4 {
		t.Fatalf("%q atom not found", name)
	}
	if bytes.Contains(data[at+4:], []byte(name)) {
		t.Fatalf("%q atom written more than once", name)
	}
	size := int(binary.BigEndian.Uint32(data[at-4 : at]))
	box := data[at-4 : at-4+size]
	if string(box[12:16]) != "data" {
		t.Fatalf("%q has no data box", name)
	}
	return binary.BigEndian.Uint32(box[16:20]), box[24:]
}

func writeTags(t *testing.T, path string, tags *MP4Tags) {
	t.Helper()
	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Write(tags, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

func TestWriteClassicalAtoms(t *testing.T) {
	data, _ := makeTaggableMP4(mdatHeader32(8+4), []byte("ABCD"))
	path := writeFixtureFile(t, "in.mp4", data)
	writeTags(t, path, classicalTags())

	cases := []struct {
		name     string
		dataType uint32
		value    []byte
	}{
		{"\xa9wrk", 1, []byte("Piano Sonata No. 16 in G Major, Op. 31/1")},
		{"\xa9mvn", 1, []byte("I. Allegro vivace")},
		{"\xa9mvi", 21, []byte{0x01, 0x02}},
		{"\xa9mvc", 21, []byte{0x01, 0x2c}},
		{"shwm", 21, []byte{0x01}},
	}
	for _, tc := range cases {
		dataType, value := ilstChild(t, path, tc.name)
		if dataType != tc.dataType || !bytes.Equal(value, tc.value) {
			t.Errorf("%q = type %d %x, want type %d %x", tc.name, dataType, value, tc.dataType, tc.value)
		}
	}
}

func TestClassicalAtomsSurviveRewrite(t *testing.T) {
	data, _ := makeTaggableMP4(mdatHeader32(8+4), []byte("ABCD"))
	path := writeFixtureFile(t, "in.mp4", data)
	writeTags(t, path, classicalTags())
	writeTags(t, path, &MP4Tags{Lyrics: string(bytes.Repeat([]byte("la "), 500))})

	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	got, err := m.Read()
	if err != nil {
		t.Fatal(err)
	}
	want := classicalTags()
	if got.Work != want.Work || got.Movement != want.Movement || got.MovementIndex != want.MovementIndex ||
		got.MovementCount != want.MovementCount || !got.ShowWorkAndMovement {
		t.Fatalf("classical fields after rewrite = %q %q %d %d %v", got.Work, got.Movement, got.MovementIndex, got.MovementCount, got.ShowWorkAndMovement)
	}
}

func TestWriteRejectsNegativeMovementNumbers(t *testing.T) {
	data, _ := makeTaggableMP4(mdatHeader32(8+4), []byte("ABCD"))
	path := writeFixtureFile(t, "in.mp4", data)
	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Write(&MP4Tags{MovementIndex: -1}, nil); err == nil {
		t.Fatal("expected error for negative movement index")
	}
}

// makeMetadataFirstMP4 places udta before trak inside moov, a layout some
// encoders produce, so the chunk offset table follows the ilst being resized.
func makeMetadataFirstMP4(payload []byte) []byte {
	ftyp := makeBox("ftyp", []byte("isom"), make([]byte, 4), []byte("isom"))
	udta := makeBox("udta", makeBox("meta", make([]byte, 4), makeBox("ilst")))
	trak := makeBox("trak", makeBox("mdia", makeBox("minf", makeBox("stbl", makeStco([]uint32{0})))))
	moov := makeBox("moov", udta, trak)
	data := append(append([]byte{}, ftyp...), moov...)
	stcoAt := bytes.Index(data, []byte("stco")) - 4
	binary.BigEndian.PutUint32(data[stcoAt+16:stcoAt+20], uint32(len(data)+8))
	data = append(data, mdatHeader32(uint32(8+len(payload)))...)
	return append(data, payload...)
}

func TestMetadataBeforeTrakKeepsChunkOffsets(t *testing.T) {
	payload := []byte("SAMPLE-DATA-0123456789")
	path := writeFixtureFile(t, "in.mp4", makeMetadataFirstMP4(payload))
	writeTags(t, path, &MP4Tags{Title: string(bytes.Repeat([]byte("t"), 200))})

	if stco, want := readStcoAt(t, path), payloadOffsetIn(t, path, payload); stco != want {
		t.Fatalf("stco offset = %d, want %d", stco, want)
	}
}
