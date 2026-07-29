package hash

import (
	"bytes"
	"testing"
)

// treeHashOfStream is what a client computes locally: one BlockHasher fed the
// whole file, then the tree hash of its block hashes.
func treeHashOfStream(t *testing.T, data []byte) string {
	t.Helper()
	h := NewBlockHasher()
	if _, err := h.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}
	return SumToHex(ComputeTreeHash(h.Sum()))
}

// treeHashOfParts is what the server computes on commit: a BlockHasher per
// uploaded part, block hashes concatenated in part order, then the tree hash.
func treeHashOfParts(t *testing.T, data []byte, partSize int) string {
	t.Helper()
	var all []byte
	for off := 0; off < len(data); off += partSize {
		h := NewBlockHasher()
		if _, err := h.Write(data[off:min(off+partSize, len(data))]); err != nil {
			t.Fatalf("write: %v", err)
		}
		all = append(all, h.Sum()...)
	}
	return SumToHex(ComputeTreeHash(all))
}

func testData(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i*31 + i/251)
	}
	return data
}

// The server only ever sees a file one part at a time, so its per-part hashing
// has to agree with a client that hashes the whole stream. That holds exactly
// when the part size is a multiple of BlockSize, which is why clients are
// required to align their chunk size.
func TestTreeHashPartSizeAlignment(t *testing.T) {
	data := testData(3*BlockSize + 12345)
	want := treeHashOfStream(t, data)

	for _, partSize := range []int{BlockSize, 2 * BlockSize, 4 * BlockSize} {
		if got := treeHashOfParts(t, data, partSize); got != want {
			t.Errorf("aligned part size %d: tree hash %s, want %s", partSize, got, want)
		}
	}

	// A part size that splits a block produces a different — and from the
	// client's point of view wrong — hash. Guards the alignment requirement.
	unaligned := BlockSize + BlockSize/2
	if got := treeHashOfParts(t, data, unaligned); got == want {
		t.Errorf("unaligned part size %d unexpectedly matched the whole-stream hash", unaligned)
	}
}

// A file smaller than one part is hashed identically either way, which is why
// dedup still works across clients that disagree about chunk size as long as
// the file fits in a single part.
func TestTreeHashSinglePart(t *testing.T) {
	data := testData(BlockSize/2 + 7)
	if got, want := treeHashOfParts(t, data, 100*1024*1024), treeHashOfStream(t, data); got != want {
		t.Errorf("single part: tree hash %s, want %s", got, want)
	}
}

// Zero-length files must hash to the tree hash of no blocks at all, since that
// is what both the client and the server's zero-size shortcut produce.
func TestTreeHashEmpty(t *testing.T) {
	empty := SumToHex(ComputeTreeHash(nil))
	if got := SumToHex(ComputeTreeHash([]byte{})); got != empty {
		t.Errorf("nil and empty block hashes disagree: %s vs %s", empty, got)
	}
	if got := treeHashOfStream(t, nil); got != empty {
		t.Errorf("empty stream: tree hash %s, want %s", got, empty)
	}
}

// Sum finalises the trailing partial block, so calling it twice must not append
// that block a second time.
func TestBlockHasherSumIdempotent(t *testing.T) {
	h := NewBlockHasher()
	if _, err := h.Write(testData(BlockSize + 1)); err != nil {
		t.Fatalf("write: %v", err)
	}
	first := h.Sum()
	if !bytes.Equal(first, h.Sum()) {
		t.Error("Sum is not idempotent")
	}
}
