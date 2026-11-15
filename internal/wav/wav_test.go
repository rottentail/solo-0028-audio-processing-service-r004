package wav

import (
	"testing"
)

// Decoding a stream must never disturb samples returned by an earlier decode,
// even when both fall into the same large size class.
func TestDecodeReturnsCallerOwnedSamples(t *testing.T) {
	count := 40000 // above the historical shared-buffer threshold
	first := make([]int16, count)
	second := make([]int16, count)
	for i := range first {
		first[i] = int16(i%60000 - 30000)
		second[i] = int16(30000 - i%60000)
	}
	firstBytes, err := Encode(Stream{Rate: 8000, Channels: 1, Samples: first})
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := Encode(Stream{Rate: 8000, Channels: 1, Samples: second})
	if err != nil {
		t.Fatal(err)
	}
	firstStream, err := Decode(firstBytes)
	if err != nil {
		t.Fatal(err)
	}
	secondStream, err := Decode(secondBytes)
	if err != nil {
		t.Fatal(err)
	}
	for i := range first {
		if firstStream.Samples[i] != first[i] {
			t.Fatalf("first decode corrupted at sample %d: got %d, want %d", i, firstStream.Samples[i], first[i])
		}
		if secondStream.Samples[i] != second[i] {
			t.Fatalf("second decode corrupted at sample %d: got %d, want %d", i, secondStream.Samples[i], second[i])
		}
	}
}

// Mutating the encoded input after Decode must not change decoded samples.
func TestDecodeCopiesOutOfEncodedBytes(t *testing.T) {
	encoded, err := Encode(Stream{Rate: 8000, Channels: 2, Samples: []int16{1, -1, 2, -2}})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for i := 44; i < len(encoded); i++ {
		encoded[i] = 0
	}
	if stream.Samples[0] != 1 || stream.Samples[3] != -2 {
		t.Fatalf("decoded samples changed with source buffer: %v", stream.Samples)
	}
}
