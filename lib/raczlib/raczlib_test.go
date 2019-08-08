// Copyright 2019 The Wuffs Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package raczlib

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"testing"

	"github.com/google/wuffs/lib/rac"
)

// These example RAC files come from "../rac/example_test.go".
//
// They are also presented in the RAC specification.
const (
	decodedMore = "" +
		"More!\n"

	decodedSheep = "" +
		"One sheep.\n" +
		"Two sheep.\n" +
		"Three sheep.\n"

	encodedMore = "" +
		"\x72\xC3\x63\x00\x78\x9C\x01\x06\x00\xF9\xFF\x4D\x6F\x72\x65\x21" +
		"\x0A\x07\x42\x01\xBF\x72\xC3\x63\x01\x65\xA9\x00\xFF\x06\x00\x00" +
		"\x00\x00\x00\x00\x01\x04\x00\x00\x00\x00\x00\x01\xFF\x35\x00\x00" +
		"\x00\x00\x00\x01\x01"

	encodedSheep = "" +
		"\x72\xC3\x63\x04\x71\xB5\x00\xFF\x00\x00\x00\x00\x00\x00\x00\xFF" +
		"\x0B\x00\x00\x00\x00\x00\x00\xFF\x16\x00\x00\x00\x00\x00\x00\xFF" +
		"\x23\x00\x00\x00\x00\x00\x00\x01\x50\x00\x00\x00\x00\x00\x01\xFF" +
		"\x5E\x00\x00\x00\x00\x00\x01\x00\x73\x00\x00\x00\x00\x00\x01\x00" +
		"\x88\x00\x00\x00\x00\x00\x01\x00\x9F\x00\x00\x00\x00\x00\x01\x04" +
		"\x08\x00\x20\x73\x68\x65\x65\x70\x2E\x0A\x0B\xE0\x02\x6E\x78\xF9" +
		"\x0B\xE0\x02\x6E\xF2\xCF\x4B\x85\x31\x01\x01\x00\x00\xFF\xFF\x17" +
		"\x21\x03\x90\x78\xF9\x0B\xE0\x02\x6E\x0A\x29\xCF\x87\x31\x01\x01" +
		"\x00\x00\xFF\xFF\x18\x0C\x03\xA8\x78\xF9\x0B\xE0\x02\x6E\x0A\xC9" +
		"\x28\x4A\x4D\x85\x71\x00\x01\x00\x00\xFF\xFF\x21\x6E\x04\x66"
)

func racCompress(original []byte, dChunkSize uint64, resourcesData [][]byte) ([]byte, error) {
	buf := &bytes.Buffer{}
	w := &rac.Writer{
		Writer:        buf,
		CodecWriter:   &CodecWriter{},
		DChunkSize:    dChunkSize,
		ResourcesData: resourcesData,
	}
	if _, err := w.Write(original); err != nil {
		return nil, fmt.Errorf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("Close: %v", err)
	}
	return buf.Bytes(), nil
}

func racDecompress(compressed []byte) ([]byte, error) {
	buf := &bytes.Buffer{}
	r := &rac.Reader{
		ReadSeeker:     bytes.NewReader(compressed),
		CompressedSize: int64(len(compressed)),
		CodecReaders:   []rac.CodecReader{&CodecReader{}},
	}
	if _, err := io.Copy(buf, r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func testReader(t *testing.T, decoded string, encoded string) {
	g, err := racDecompress([]byte(encoded))
	if err != nil {
		t.Fatalf("racDecompress: %v", err)
	}
	if got, want := string(g), decoded; got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestReaderSansDictionary(t *testing.T) { testReader(t, decodedMore, encodedMore) }
func TestReaderWithDictionary(t *testing.T) { testReader(t, decodedSheep, encodedSheep) }

func TestReaderConcatenation(t *testing.T) {
	// Create a RAC file whose decoding is the concatenation of two other RAC
	// file's decoding. The resultant RAC file's contents (the encoded form) is
	// the concatenation of the two RAC files, plus a new root node.

	const rootNodeRelativeOffset0 = 0x00 // Sheep's root node is at its start.
	const rootNodeRelativeOffset1 = 0x15 // More's  root node is at its end.
	decLen0 := uint64(len(decodedSheep))
	decLen1 := uint64(len(decodedMore))
	encLen0 := uint64(len(encodedSheep))
	encLen1 := uint64(len(encodedMore))

	// Define a buffer to hold a new root node with 3 children: 1 metadata node
	// and 2 branch nodes. The metadata node (one whose DRange is empty) is
	// required because one of the original RAC files' root node is not located
	// at its start. Walking to that child branch node needs two COffset
	// values: one for the embedded RAC file's start and one for the embedded
	// RAC file's root node.
	//
	// Whether the metadata node is the first, middle or last child is
	// arbitrary. We choose to make it the first child.
	const arity = 3
	buf := [16 * (arity + 1)]byte{}

	// DPtr values.
	binary.LittleEndian.PutUint64(buf[0x00:], 0)
	binary.LittleEndian.PutUint64(buf[0x08:], 0)
	binary.LittleEndian.PutUint64(buf[0x10:], 0+decLen0)
	binary.LittleEndian.PutUint64(buf[0x18:], 0+decLen0+decLen1)

	// CPtr values.
	binary.LittleEndian.PutUint64(buf[0x20:], encLen0)
	binary.LittleEndian.PutUint64(buf[0x28:], 0)
	binary.LittleEndian.PutUint64(buf[0x30:], encLen0+rootNodeRelativeOffset1)
	binary.LittleEndian.PutUint64(buf[0x38:], encLen0+encLen1+uint64(len(buf)))

	// Magic and Arity.
	buf[0x00] = 0x72
	buf[0x01] = 0xC3
	buf[0x02] = 0x63
	buf[0x03] = arity
	buf[0x3F] = arity

	// TTag values.
	buf[0x07] = 0xFF // Unused (metadata node).
	buf[0x0F] = 0xFE // Branch node.
	buf[0x17] = 0xFE // Branch node.

	// CLen values.
	buf[0x26] = 0x00 // Unused (metadata node).
	buf[0x2E] = 0x04 // Branch node, which is always at most 4 KiB in size.
	buf[0x36] = 0x04 // Branch node, which is always at most 4 KiB in size.

	// STag values.
	buf[0x27] = 0xFF // Unused (metadata node).
	buf[0x2F] = 0x01 // CBiasing with COff[1], which is 0.
	buf[0x37] = 0x00 // CBiasing with COff[0], which is encLen0.

	// Codec and Version.
	buf[0x1F] = byte(rac.CodecZlib)
	buf[0x3E] = 0x01

	// Checksum.
	checksum := crc32.ChecksumIEEE(buf[6:])
	checksum ^= checksum >> 16
	buf[0x04] = byte(checksum >> 0)
	buf[0x05] = byte(checksum >> 8)

	// Test the concatenation.
	testReader(t,
		decodedSheep+decodedMore,
		encodedSheep+encodedMore+string(buf[:]),
	)
}

func TestZeroedBytes(t *testing.T) {
	original := []byte("abcde\x00\x00\x00\x00j")
	compressed, err := racCompress(original, 8, nil)
	if err != nil {
		t.Fatal(err)
	}

	r := &rac.Reader{
		ReadSeeker:     bytes.NewReader(compressed),
		CompressedSize: int64(len(compressed)),
		CodecReaders:   []rac.CodecReader{&CodecReader{}},
	}
	for i := 0; i <= len(original); i++ {
		want := original[i:]
		got := make([]byte, len(want))
		for j := range got {
			got[j] = '?'
		}

		if _, err := r.Seek(int64(i), io.SeekStart); err != nil {
			t.Errorf("i=%d: Seek: %v", i, err)
			continue
		}
		if _, err := io.ReadFull(r, got); err != nil {
			t.Errorf("i=%d: ReadFull: %v", i, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("i=%d: got\n% 02x\nwant\n% 02x", i, got, want)
			continue
		}
	}
}

func TestSharedDictionary(t *testing.T) {
	// Make some "dictionary" data that, as an independent chunk, does not
	// compress very well.
	const n = 256
	dictionary := make([]byte, n)
	for i := range dictionary {
		dictionary[i] = uint8(i)
	}

	// Replicate it 32 times.
	original := make([]byte, 0, 32*n)
	for len(original) < 32*n {
		original = append(original, dictionary...)
	}

	// Measure the RAC-compressed form of that replicated data, without and
	// with a shared dictionary.
	compressedLengths := [2]int{}
	for i := range compressedLengths {
		resourcesData := [][]byte{}
		if i > 0 {
			resourcesData = [][]byte{dictionary}
		}

		// Compress.
		compressed, err := racCompress(original, n, resourcesData)
		if err != nil {
			t.Fatalf("i=%d: racCompress: %v", i, err)
		}
		if len(compressed) == 0 {
			t.Fatalf("i=%d: compressed form is empty", i)
		}
		compressedLengths[i] = len(compressed)

		// Decompress.
		decompressed, err := racDecompress(compressed)
		if err != nil {
			t.Fatalf("i=%d: racDecompress: %v", i, err)
		}
		if !bytes.Equal(decompressed, original) {
			t.Fatalf("i=%d: racDecompress: round trip did not match original", i)
		}
	}

	// Using a shared dictionary should improve the compression ratio. The
	// exact value depends on the Zlib compression algorithm, but we should
	// expect at least a 4x improvement.
	if ratio := compressedLengths[0] / compressedLengths[1]; ratio < 4 {
		t.Fatalf("ratio: got %dx, want at least 4x", ratio)
	}
}
