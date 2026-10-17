package main

const (
	appName             = "DJMAX Audio Watcher"
	mutexName           = "Local\\DJMAXAudioWatcher_Mutex"
	eventName           = "Local\\DJMAXAudioWatcher_Stop"
	taskName            = "DJMAXAudioWatcher"
	trayWindowClassName = "DJMAXAudioWatcherTrayWindow"
)

// appVersion is replaced at build time with: -X main.appVersion=<git-tag>.
// Development builds intentionally identify themselves as dev.
var appVersion = "dev"

type instanceIdentity struct {
	version string
	mutex   string
	event   string
}

// These aliases keep released and older development builds from running at
// the same time as the stable, version-independent instance identity.
var legacyInstances = []instanceIdentity{
	{version: "v2", mutex: "Local\\DJMAXAudioWatcherV2_Mutex", event: "Local\\DJMAXAudioWatcherV2_Stop"},
	{version: "v3", mutex: "Local\\DJMAXAudioWatcherV3_Mutex", event: "Local\\DJMAXAudioWatcherV3_Stop"},
	{version: "v4", mutex: "Local\\DJMAXAudioWatcherV4_Mutex", event: "Local\\DJMAXAudioWatcherV4_Stop"},
	{version: "v5", mutex: "Local\\DJMAXAudioWatcherV5_Mutex", event: "Local\\DJMAXAudioWatcherV5_Stop"},
}

var legacyTaskNames = []string{"DJMAXAudioWatcherV2"}

func appTitle() string {
	version := appVersion
	if version == "" {
		version = "dev"
	}
	return appName + " " + version
}
