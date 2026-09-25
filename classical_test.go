package mp4tag

import (
	"bytes"
	"encoding/binary"
	"math"
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

// ilstItem builds an ilst item holding one data box of the given type.
func ilstItem(name string, dataType uint32, value []byte) []byte {
	typeAndLocale := make([]byte, 8)
	binary.BigEndian.PutUint32(typeAndLocale[0:4], dataType)
	return makeBox(name, makeBox("data", typeAndLocale, value))
}

func TestReadToleratesIntegerAtomsFromOtherTaggers(t *testing.T) {
	cases := []struct {
		name         string
		item         []byte
		index, count int16
		show         bool
	}{
		{"4-byte index", ilstItem("\xa9mvi", 21, []byte{0, 0, 0, 3}), 3, 0, false},
		{"3-byte count", ilstItem("\xa9mvc", 21, []byte{0, 0, 7}), 0, 7, false},
		{"8-byte count", ilstItem("\xa9mvc", 21, []byte{0, 0, 0, 0, 0, 0, 0, 12}), 0, 12, false},
		{"2-byte flag", ilstItem("shwm", 21, []byte{0, 1}), 0, 0, true},
		{"implicit type", ilstItem("shwm", 0, []byte{1}), 0, 0, true},
		{"index above int16", ilstItem("\xa9mvi", 21, []byte{0, 0, 0x9c, 0x40}), math.MaxInt16, 0, false},
		{"negative index", ilstItem("\xa9mvi", 21, []byte{0xff, 0xff}), 0, 0, false},
		{"text index", ilstItem("\xa9mvi", 1, []byte("12")), 0, 0, false},
		{"9-byte index", ilstItem("\xa9mvi", 21, make([]byte, 9)), 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := makeTaggableMP4(mdatHeader32(8+4), []byte("ABCD"), tc.item)
			m, err := Open(writeFixtureFile(t, "in.mp4", data))
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			check := func(step string) *MP4Tags {
				t.Helper()
				got, err := m.Read()
				if err != nil {
					t.Fatalf("%s: %v", step, err)
				}
				if got.MovementIndex != tc.index || got.MovementCount != tc.count || got.ShowWorkAndMovement != tc.show {
					t.Fatalf("%s: index %d, count %d, show %v; want %d, %d, %v", step,
						got.MovementIndex, got.MovementCount, got.ShowWorkAndMovement, tc.index, tc.count, tc.show)
				}
				return got
			}

			// A caller changing one field writes back everything it read.
			tags := check("Read")
			tags.Title = "x"
			if err := m.Write(tags, nil); err != nil {
				t.Fatalf("Write: %v", err)
			}
			check("Read after Write")
		})
	}
}

func TestWriteNilTagsDeletesClassicalAtoms(t *testing.T) {
	data, _ := makeTaggableMP4(mdatHeader32(8+4), []byte("ABCD"))
	path := writeFixtureFile(t, "in.mp4", data)
	writeTags(t, path, classicalTags())

	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	del := []string{"work", "movement", "movementindex", "movementcount", "showworkandmovement"}
	if err := m.Write(nil, del); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := m.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Work != "" || got.Movement != "" || got.MovementIndex != 0 || got.MovementCount != 0 || got.ShowWorkAndMovement {
		t.Fatalf("classical fields after delete = %q %q %d %d %v", got.Work, got.Movement, got.MovementIndex, got.MovementCount, got.ShowWorkAndMovement)
	}
	if got.Title != classicalTags().Title {
		t.Fatalf("title = %q, want it kept", got.Title)
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
// encoders produce, so the chunk offset table (the named stco or co64)
// follows the ilst being resized. ilst starts out holding items.
func makeMetadataFirstMP4(table string, payload []byte, items ...[]byte) []byte {
	ftyp := makeBox("ftyp", []byte("isom"), make([]byte, 4), []byte("isom"))
	udta := makeBox("udta", makeBox("meta", make([]byte, 4), makeBox("ilst", items...)))
	offsets := makeStco([]uint32{0})
	if table == "co64" {
		offsets = makeCo64([]uint64{0})
	}
	trak := makeBox("trak", makeBox("mdia", makeBox("minf", makeBox("stbl", offsets))))
	moov := makeBox("moov", udta, trak)
	data := append(append([]byte{}, ftyp...), moov...)
	payloadAt := len(data) + 8
	entry := bytes.Index(data, []byte(table)) + 12
	if table == "co64" {
		binary.BigEndian.PutUint64(data[entry:entry+8], uint64(payloadAt))
	} else {
		binary.BigEndian.PutUint32(data[entry:entry+4], uint32(payloadAt))
	}
	data = append(data, mdatHeader32(uint32(8+len(payload)))...)
	return append(data, payload...)
}

// firstChunkOffset returns the first entry of the named chunk offset table.
func firstChunkOffset(t *testing.T, path, table string) uint64 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := bytes.Index(data, []byte(table)) + 12
	if table == "co64" {
		return binary.BigEndian.Uint64(data[entry : entry+8])
	}
	return uint64(binary.BigEndian.Uint32(data[entry : entry+4]))
}

func TestMetadataBeforeTrakKeepsChunkOffsets(t *testing.T) {
	payload := []byte("SAMPLE-DATA-0123456789")
	long := string(bytes.Repeat([]byte("t"), 200))
	cases := []struct {
		name, table   string
		before, after string // titles before and after the write
	}{
		{"stco grows", "stco", "", long},
		{"stco shrinks", "stco", long, "t"},
		{"co64 grows", "co64", "", long},
		{"co64 shrinks", "co64", long, "t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var items [][]byte
			if tc.before != "" {
				items = append(items, ilstItem("\xa9nam", 1, []byte(tc.before)))
			}
			path := writeFixtureFile(t, "in.mp4", makeMetadataFirstMP4(tc.table, payload, items...))
			writeTags(t, path, &MP4Tags{Title: tc.after})

			if got, want := firstChunkOffset(t, path, tc.table), uint64(payloadOffsetIn(t, path, payload)); got != want {
				t.Fatalf("%s offset = %d, want %d", tc.table, got, want)
			}
		})
	}
}
