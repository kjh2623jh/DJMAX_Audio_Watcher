//go:build windows

package main

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestAppTitle(t *testing.T) {
	original := appVersion
	defer func() { appVersion = original }()

	for _, tc := range []struct {
		version string
		want    string
	}{
		{version: "v1.0.2", want: "DJMAX Audio Watcher v1.0.2"},
		{version: "dev", want: "DJMAX Audio Watcher dev"},
		{version: "", want: "DJMAX Audio Watcher dev"},
	} {
		appVersion = tc.version
		if got := appTitle(); got != tc.want {
			t.Fatalf("appTitle() = %q, want %q", got, tc.want)
		}
	}
}

func TestCurrentInstanceNamesAreVersionIndependent(t *testing.T) {
	for label, value := range map[string]string{
		"mutex":       mutexName,
		"event":       eventName,
		"task":        taskName,
		"tray window": trayWindowClassName,
	} {
		if strings.Contains(strings.ToLower(value), "v5") {
			t.Fatalf("%s name still contains a version: %q", label, value)
		}
	}
}

func TestTaskScriptMigratesAndRemovesLegacyTask(t *testing.T) {
	install := taskScript(true, `C:\Program Files\DJMAX Audio Watcher\DJMAX_Audio_Watcher.exe`)
	if !strings.Contains(install, "Register-ScheduledTask -TaskName '"+taskName+"'") {
		t.Fatalf("install script does not register stable task: %s", install)
	}
	for _, legacy := range legacyTaskNames {
		if !strings.Contains(install, "Unregister-ScheduledTask -TaskName '"+legacy+"'") {
			t.Fatalf("install script does not remove legacy task %q: %s", legacy, install)
		}
	}

	uninstall := taskScript(false, "ignored")
	for _, name := range allTaskNames() {
		if !strings.Contains(uninstall, "Unregister-ScheduledTask -TaskName '"+name+"'") {
			t.Fatalf("uninstall script does not remove task %q: %s", name, uninstall)
		}
	}
}

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
