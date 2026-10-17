//go:build windows

package main

import (
	"encoding/binary"
	"testing"
)

func TestIs48kStereo(t *testing.T) {
	cases := []struct {
		name string
		f    formatInfo
		want bool
	}{
		{"24bit stereo", formatInfo{rate: 48000, bits: 24, channels: 2}, true},
		{"32bit stereo", formatInfo{rate: 48000, bits: 32, channels: 2}, true},
		{"16bit stereo", formatInfo{rate: 48000, bits: 16, channels: 2}, true},
		{"192kHz stereo", formatInfo{rate: 192000, bits: 32, channels: 2}, false},
		{"48kHz mono", formatInfo{rate: 48000, bits: 24, channels: 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := is48kStereo(tc.f); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	b := make([]byte, 40)
	binary.LittleEndian.PutUint16(b[0:2], 0xfffe)
	binary.LittleEndian.PutUint16(b[2:4], 2)
	binary.LittleEndian.PutUint32(b[4:8], 192000)
	binary.LittleEndian.PutUint16(b[14:16], 32)
	binary.LittleEndian.PutUint32(b[24:28], 1)
	got, err := parseFormat(b)
	if err != nil {
		t.Fatal(err)
	}
	want := formatInfo{rate: 192000, bits: 32, channels: 2, subtype: 1}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
