package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	appDirName                        = "DJMAXAudioWatcher"
	gameName                          = "DJMAX RESPECT V.exe"
	device                            = "DefaultRenderDevice"
	mutexName                         = "Local\\DJMAXAudioWatcherV5_Mutex"
	eventName                         = "Local\\DJMAXAudioWatcherV5_Stop"
	taskName                          = "DJMAXAudioWatcherV2"
	ERROR_ALREADY_EXISTS              = 183
	TH32CS_SNAPPROCESS                = 0x00000002
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	SYNCHRONIZE                       = 0x00100000
	EVENT_MODIFY_STATE                = 0x0002
	WAIT_OBJECT_0                     = 0
	INFINITE                          = 0xffffffff
	MB_YESNO                          = 0x00000004
	MB_YESNOCANCEL                    = 0x00000003
	MB_ICONQUESTION                   = 0x00000020
	MB_ICONERROR                      = 0x00000010
	IDYES                             = 6
	IDNO                              = 7
	IDCANCEL                          = 2
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	user32                  = syscall.NewLazyDLL("user32.dll")
	shell32                 = syscall.NewLazyDLL("shell32.dll")
	procCreateMutex         = kernel32.NewProc("CreateMutexW")
	procOpenMutex           = kernel32.NewProc("OpenMutexW")
	procCreateEvent         = kernel32.NewProc("CreateEventW")
	procOpenEvent           = kernel32.NewProc("OpenEventW")
	procSetEvent            = kernel32.NewProc("SetEvent")
	procWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
	procOpenProcess         = kernel32.NewProc("OpenProcess")
	procMessageBox          = user32.NewProc("MessageBoxW")
	procIsUserAnAdmin       = shell32.NewProc("IsUserAnAdmin")
	procShellExecute        = shell32.NewProc("ShellExecuteW")
	procCreateSnapshot      = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32First      = kernel32.NewProc("Process32FirstW")
	procProcess32Next       = kernel32.NewProc("Process32NextW")
)

type processEntry32 struct {
	Size            uint32
	Usage           uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	Threads         uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

type formatInfo struct {
	rate     uint32
	bits     uint16
	channels uint16
	subtype  uint32
}

func parseFormat(b []byte) (formatInfo, error) {
	if len(b) < 16 {
		return formatInfo{}, fmt.Errorf("format backup too short: %d bytes", len(b))
	}
	f := formatInfo{rate: binary.LittleEndian.Uint32(b[4:8]), bits: binary.LittleEndian.Uint16(b[14:16]), channels: binary.LittleEndian.Uint16(b[2:4])}
	if len(b) >= 28 && binary.LittleEndian.Uint16(b[:2]) == 0xfffe {
		f.subtype = binary.LittleEndian.Uint32(b[24:28])
	}
	if f.rate < 8000 || f.rate > 768000 || f.channels < 1 || f.channels > 32 {
		return f, errors.New("invalid saved format")
	}
	return f, nil
}
func (f formatInfo) String() string {
	return fmt.Sprintf("%dHz %d-bit %dch subtype=%d", f.rate, f.bits, f.channels, f.subtype)
}

func wide(s string) *uint16 { return syscall.StringToUTF16Ptr(s) }
func closeHandle(h uintptr) {
	if h != 0 && h != ^uintptr(0) {
		procCloseHandle.Call(h)
	}
}
func msg(text string, flags uintptr) uintptr {
	r, _, _ := procMessageBox.Call(0, uintptr(unsafe.Pointer(wide(text))), uintptr(unsafe.Pointer(wide("DJMAX Audio Watcher v5"))), flags)
	return r
}
func elevated() bool { r, _, _ := procIsUserAnAdmin.Call(); return r != 0 }
func elevateSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	wd := filepath.Dir(exe)
	args := strings.Join(os.Args[1:], " ")
	r, _, e := procShellExecute.Call(0, uintptr(unsafe.Pointer(wide("runas"))), uintptr(unsafe.Pointer(wide(exe))), uintptr(unsafe.Pointer(wide(args))), uintptr(unsafe.Pointer(wide(wd))), 0)
	if r <= 32 {
		return fmt.Errorf("administrator elevation failed: %v (code %d)", e, r)
	}
	return nil
}
func processIDs() (map[uint32]bool, error) {
	h, _, e := procCreateSnapshot.Call(TH32CS_SNAPPROCESS, 0)
	if h == ^uintptr(0) {
		return nil, fmt.Errorf("CreateToolhelp32Snapshot: %v", e)
	}
	defer closeHandle(h)
	p := processEntry32{Size: uint32(unsafe.Sizeof(processEntry32{}))}
	result := make(map[uint32]bool)
	r, _, _ := procProcess32First.Call(h, uintptr(unsafe.Pointer(&p)))
	if r == 0 {
		return nil, errors.New("Process32FirstW failed")
	}
	for r != 0 {
		name := syscall.UTF16ToString(p.ExeFile[:])
		if strings.EqualFold(name, gameName) {
			result[p.ProcessID] = true
		}
		r, _, _ = procProcess32Next.Call(h, uintptr(unsafe.Pointer(&p)))
	}
	return result, nil
}
func idString(ids map[uint32]bool) string {
	if len(ids) == 0 {
		return "none"
	}
	v := make([]string, 0, len(ids))
	for pid := range ids {
		v = append(v, strconv.FormatUint(uint64(pid), 10))
	}
	return strings.Join(v, ",")
}

type app struct {
	tracked        map[uint32]bool
	log            *log.Logger
	logFile        *os.File
	svcl           string
	backup         string
	dataDir        string
	active         bool
	currentIDs     map[uint32]bool
	restoreRetries int
	trayRunning    atomic.Bool
}

func (a *app) runSVCL(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.svcl, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("svcl %q: %w; output=%q", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	if len(bytes.TrimSpace(out)) > 0 {
		a.log.Printf("SVCL output %q: %q", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}
func (a *app) readCurrent() (formatInfo, error) {
	tmp := filepath.Join(a.dataDir, "format-probe.dat")
	defer os.Remove(tmp)
	os.Remove(tmp)
	if err := a.runSVCL("/SaveDeviceFormat", device, tmp); err != nil {
		return formatInfo{}, err
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		return formatInfo{}, err
	}
	return parseFormat(b)
}
func (a *app) saveOriginal() error {
	if b, err := os.ReadFile(a.backup); err == nil {
		f, e := parseFormat(b)
		if e != nil {
			return fmt.Errorf("existing backup is invalid (not overwriting): %w", e)
		}
		a.log.Printf("Reusing existing original format backup: %s", f.String())
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp := a.backup + ".tmp"
	os.Remove(tmp)
	if err := a.runSVCL("/SaveDeviceFormat", device, tmp); err != nil {
		return err
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	f, err := parseFormat(b)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, a.backup); err != nil {
		return err
	}
	a.log.Printf("Saved original output format: %s", f.String())
	return nil
}

// is48kStereo is intentionally independent of bit depth. Some drivers expose
// 32-bit PCM even after SoundVolumeCommandLine requests 24-bit output.
func is48kStereo(f formatInfo) bool {
	return f.rate == 48000 && f.channels == 2
}

func (a *app) switch48(reason string) {
	// Always preserve the pre-game format before changing the device. If a
	// backup exists from an interrupted run, never overwrite it with 48 kHz.
	if err := a.saveOriginal(); err != nil {
		a.log.Printf("ERROR backup before switch: %v", err)
		return
	}
	a.active = true
	a.trayRunning.Store(true)

	// Query first: even setting the *same* format can invalidate an existing
	// WASAPI stream in browsers. Never call SetDefaultFormat unnecessarily.
	current, err := a.readCurrent()
	if err != nil {
		a.log.Printf("WARNING pre-switch format probe failed (%s): %v; attempting requested format", reason, err)
	} else {
		a.log.Printf("DJMAX START (%s): pre-switch output format: %s", reason, current.String())
		if is48kStereo(current) {
			a.log.Printf("DJMAX START (%s): SKIP SetDefaultFormat (already 48kHz stereo)", reason)
			return
		}
	}

	// 24-bit is the preferred request; use other depths only if 48 kHz
	// stereo was NOT observed. Do not force a second reset just because a
	// driver reports 32-bit PCM after accepting the 24-bit request.
	for _, bits := range []int{24, 32, 16} {
		started := time.Now()
		a.log.Printf("DJMAX START (%s): SetDefaultFormat request %d-bit / 48000Hz / 2ch", reason, bits)
		err := a.runSVCL("/SetDefaultFormat", device, strconv.Itoa(bits), "48000", "2")
		if err != nil {
			a.log.Printf("48kHz set attempt %d-bit failed after %s: %v", bits, time.Since(started).Round(time.Millisecond), err)
			// A nonzero exit code does not guarantee the format was unchanged.
			// Verify before considering another write.
		}
		observed, verifyErr := a.readCurrent()
		if verifyErr != nil {
			a.log.Printf("48kHz verification error (%d-bit): %v", bits, verifyErr)
			continue
		}
		if is48kStereo(observed) {
			a.log.Printf("DJMAX START (%s): 48kHz verified after %d-bit request (%s): %s", reason, bits, time.Since(started).Round(time.Millisecond), observed.String())
			return
		}
		a.log.Printf("48kHz verification mismatch after %d-bit attempt: %s", bits, observed.String())
	}
	a.log.Printf("ERROR: unable to verify 48kHz after DJMAX START (%s); backup preserved", reason)
}
func (a *app) restore(reason string) bool {
	b, err := os.ReadFile(a.backup)
	if os.IsNotExist(err) {
		a.active = false
		a.trayRunning.Store(false)
		return true
	}
	if err != nil {
		a.log.Printf("ERROR reading backup: %v", err)
		return false
	}
	want, err := parseFormat(b)
	if err != nil {
		a.log.Printf("ERROR backup corrupted, preserving it: %v", err)
		return false
	}
	// Avoid resetting the Windows audio engine if the original format is
	// already active. This also handles recovery from an interrupted run.
	got, probeErr := a.readCurrent()
	if probeErr == nil && got == want {
		if err := os.Remove(a.backup); err != nil {
			a.log.Printf("ERROR removing already-restored backup: %v", err)
			return false
		}
		a.active = false
		a.trayRunning.Store(false)
		a.restoreRetries = 0
		a.log.Printf("DJMAX STOP (%s): SKIP LoadDeviceFormat (already original format): %s", reason, got.String())
		return true
	}
	if probeErr != nil {
		a.log.Printf("WARNING pre-restore format probe failed (%s): %v; restoring from backup", reason, probeErr)
	} else {
		a.log.Printf("DJMAX STOP (%s): pre-restore output format: %s", reason, got.String())
	}
	if err := a.runSVCL("/LoadDeviceFormat", device, a.backup); err != nil {
		a.log.Printf("ERROR restore (%s): %v", reason, err)
		return false
	}
	got, err = a.readCurrent()
	if err != nil {
		a.log.Printf("ERROR verifying restore (%s): %v", reason, err)
		return false
	}
	if got != want {
		a.log.Printf("ERROR restore verification mismatch (%s): want %s, got %s; backup retained", reason, want.String(), got.String())
		return false
	}
	if err := os.Remove(a.backup); err != nil {
		a.log.Printf("ERROR removing restored backup: %v", err)
		return false
	}
	a.active = false
	a.trayRunning.Store(false)
	a.restoreRetries = 0
	a.log.Printf("DJMAX STOP (%s): original format restored and verified: %s", reason, got.String())
	return true
}

const wmiScript = `$ErrorActionPreference = 'Stop'
$start = 'DJMAXWatcherV5.Start'
$stop = 'DJMAXWatcherV5.Stop'
try {
  Register-WmiEvent -Query 'SELECT * FROM Win32_ProcessStartTrace' -SourceIdentifier $start | Out-Null
  Register-WmiEvent -Query 'SELECT * FROM Win32_ProcessStopTrace' -SourceIdentifier $stop | Out-Null
  [Console]::Out.WriteLine('READY'); [Console]::Out.Flush()
  while ($true) {
    $e = Wait-Event
    if ($null -eq $e) { continue }
    $kind = if ($e.SourceIdentifier -eq $start) { 'START' } elseif ($e.SourceIdentifier -eq $stop) { 'STOP' } else { '' }
    if ($kind -ne '') {
      $p = $e.SourceEventArgs.NewEvent
      $name = [string]$p.ProcessName
      if ($kind -eq 'STOP' -or $name -ieq 'DJMAX RESPECT V.exe') {
        $pidNumber = [string]$p.ProcessID
        [Console]::Out.WriteLine($kind + '|' + $pidNumber)
        [Console]::Out.Flush()
      }
    }
    Remove-Event -EventIdentifier $e.EventIdentifier -ErrorAction SilentlyContinue
  }
} catch {
  [Console]::Error.WriteLine($_.ToString()); [Console]::Error.Flush(); exit 1
} finally {
  Unregister-Event -SourceIdentifier $start -ErrorAction SilentlyContinue
  Unregister-Event -SourceIdentifier $stop -ErrorAction SilentlyContinue
}`

type wmiEvent struct {
	kind string
	pid  uint32
	info string
}

func listenWMI(ctx context.Context, a *app, events chan<- wmiEvent) {
	for {
		if ctx.Err() != nil {
			return
		}
		cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", wmiScript)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			a.log.Printf("ERROR stdout pipe: %v", err)
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			a.log.Printf("ERROR stderr pipe: %v", err)
			return
		}
		if err := cmd.Start(); err != nil {
			a.log.Printf("ERROR launching WMI listener: %v", err)
			return
		}
		a.log.Printf("WMI listener started (PID %d)", cmd.Process.Pid)
		doneErr := make(chan string, 1)
		go func() {
			data, _ := io.ReadAll(io.LimitReader(stderr, 8192))
			doneErr <- strings.TrimSpace(string(data))
		}()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 64*1024)
		for scanner.Scan() {
			s := strings.TrimSpace(scanner.Text())
			if s == "READY" {
				select {
				case events <- wmiEvent{kind: "READY"}:
				case <-ctx.Done():
				}
				continue
			}
			pieces := strings.SplitN(s, "|", 2)
			if len(pieces) == 2 && (pieces[0] == "START" || pieces[0] == "STOP") {
				pid, e := strconv.ParseUint(pieces[1], 10, 32)
				if e == nil {
					select {
					case events <- wmiEvent{kind: pieces[0], pid: uint32(pid)}:
					case <-ctx.Done():
					}
					continue
				}
			}
			select {
			case events <- wmiEvent{kind: "INFO", info: s}:
			case <-ctx.Done():
			}
		}
		if err := scanner.Err(); err != nil {
			a.log.Printf("WMI stdout scan error: %v", err)
		}
		err = cmd.Wait()
		errText := <-doneErr
		if ctx.Err() != nil {
			return
		}
		a.log.Printf("WMI listener exited: %v; stderr=%q; restarting in 2s", err, errText)
		select {
		case events <- wmiEvent{kind: "DISCONNECTED"}:
		case <-ctx.Done():
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

// Track termination using the Windows process object. No process-list polling is needed.
// Win32_ProcessStopTrace is not reliable on all machines; kernel handles are the primary signal.
func (a *app) watchPID(pid uint32, events chan<- wmiEvent) {
	if a.tracked[pid] {
		return
	}
	h, _, er := procOpenProcess.Call(SYNCHRONIZE, 0, uintptr(pid))
	if h == 0 {
		a.log.Printf("Process-handle watch failed PID=%d: %v; scheduling reconciliation", pid, er)
		select {
		case events <- wmiEvent{kind: "WATCHFAIL", pid: pid}:
		default:
		}
		return
	}
	a.tracked[pid] = true
	a.log.Printf("Process-handle watch attached PID=%d", pid)
	go func() {
		defer closeHandle(h)
		r, _, err := procWaitForSingleObject.Call(h, INFINITE)
		if r == WAIT_OBJECT_0 {
			select {
			case events <- wmiEvent{kind: "EXIT", pid: pid}:
			default:
			}
		} else {
			select {
			case events <- wmiEvent{kind: "WATCHERROR", pid: pid, info: fmt.Sprintf("WaitForSingleObject result=%d err=%v", r, err)}:
			default:
			}
		}
	}()
}
func (a *app) watchCurrent(events chan<- wmiEvent) {
	for pid := range a.currentIDs {
		a.watchPID(pid, events)
	}
}

func (a *app) reconcile(reason string) bool {
	ids, err := processIDs()
	if err != nil {
		a.log.Printf("ERROR process snapshot (%s): %v", reason, err)
		return false
	}
	a.currentIDs = ids
	a.log.Printf("Process reconciliation (%s): game PIDs=%s, active=%t", reason, idString(ids), a.active)
	if len(ids) > 0 {
		if !a.active {
			a.switch48(reason)
		}
		return true
	}
	if a.active {
		a.restore(reason)
	} else if _, err := os.Stat(a.backup); err == nil {
		a.restore("orphan backup / " + reason)
	}
	return false
}

func taskScript(install bool, exe string) string {
	if !install {
		return "Unregister-ScheduledTask -TaskName '" + taskName + "' -Confirm:$false -ErrorAction SilentlyContinue"
	}
	quoted := strings.ReplaceAll(exe, "'", "''")
	return fmt.Sprintf("$a=New-ScheduledTaskAction -Execute '%s' -Argument '--startup'; $t=New-ScheduledTaskTrigger -AtLogOn -User ([Security.Principal.WindowsIdentity]::GetCurrent().Name); $p=New-ScheduledTaskPrincipal -UserId ([Security.Principal.WindowsIdentity]::GetCurrent().Name) -LogonType Interactive -RunLevel Highest; Register-ScheduledTask -TaskName '%s' -Action $a -Trigger $t -Principal $p -Force | Out-Null", quoted, taskName)
}
func setStartup(install bool, exe string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", taskScript(install, exe))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("scheduled task update failed: %w, %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		msg(err.Error(), MB_ICONERROR)
		return
	}
	exe, _ = filepath.Abs(exe)
	args := os.Args[1:]
	if !elevated() {
		if err := elevateSelf(); err != nil {
			msg("관리자 권한 실행 실패:\n"+err.Error(), MB_ICONERROR)
		}
		return
	}
	// Previous versions use distinct mutex names. Never let two versions
	// reconfigure the same audio endpoint concurrently.
	for _, old := range []struct{ version, mutex string }{
		{"v2", "Local\\DJMAXAudioWatcherV2_Mutex"},
		{"v3", "Local\\DJMAXAudioWatcherV3_Mutex"},
		{"v4", "Local\\DJMAXAudioWatcherV4_Mutex"},
	} {
		h, _, _ := procOpenMutex.Call(SYNCHRONIZE, 0, uintptr(unsafe.Pointer(wide(old.mutex))))
		if h != 0 {
			closeHandle(h)
			msg("이전 버전("+old.version+")이 실행 중입니다.\n트레이 메뉴 또는 기존 EXE에서 정상 종료한 뒤 v5를 실행해 주세요.", MB_ICONERROR)
			return
		}
	}
	// A second invocation only sends a graceful stop signal to the existing instance.
	mh, _, e := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(wide(mutexName))))
	if mh == 0 {
		msg("단일 실행 잠금 생성 실패: "+e.Error(), MB_ICONERROR)
		return
	}
	defer closeHandle(mh)
	if e == syscall.Errno(ERROR_ALREADY_EXISTS) {
		if msg("이미 DJMAX 감시 프로그램이 실행 중입니다.\n기존 프로그램을 정상 종료할까요?", MB_YESNO|MB_ICONQUESTION) == IDYES {
			eh, _, er := procOpenEvent.Call(EVENT_MODIFY_STATE, 0, uintptr(unsafe.Pointer(wide(eventName))))
			if eh == 0 {
				msg("종료 신호를 보낼 수 없습니다: "+er.Error(), MB_ICONERROR)
			} else {
				procSetEvent.Call(eh)
				closeHandle(eh)
			}
		}
		return
	}
	if len(args) > 0 && args[0] == "--uninstall" {
		if err := setStartup(false, exe); err != nil {
			msg(err.Error(), MB_ICONERROR)
		} else {
			msg("자동 실행 작업을 삭제했습니다.", 0)
		}
		return
	}
	if len(args) > 0 && args[0] == "--install" {
		if err := setStartup(true, exe); err != nil {
			msg(err.Error(), MB_ICONERROR)
		} else {
			msg("로그인 시 관리자 권한 자동 실행 작업을 등록했습니다.", 0)
		}
		return
	}
	eh, _, er := procCreateEvent.Call(0, 1, 0, uintptr(unsafe.Pointer(wide(eventName))))
	if eh == 0 {
		msg("종료 이벤트 생성 실패: "+er.Error(), MB_ICONERROR)
		return
	}
	defer closeHandle(eh)
	svcl := filepath.Join(filepath.Dir(exe), "svcl.exe")
	if _, err := os.Stat(svcl); err != nil {
		msg("svcl.exe를 감시 프로그램과 같은 폴더에 넣어 주세요.\n\n"+svcl, MB_ICONERROR)
		return
	}
	dataDir := filepath.Join(os.Getenv("LOCALAPPDATA"), appDirName)
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		msg(err.Error(), MB_ICONERROR)
		return
	}
	logFile, err := os.OpenFile(filepath.Join(dataDir, "watcher.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		msg(err.Error(), MB_ICONERROR)
		return
	}
	defer logFile.Close()
	a := &app{log: log.New(logFile, "", log.Ldate|log.Ltime), logFile: logFile, svcl: svcl, backup: filepath.Join(dataDir, "original-format.dat"), dataDir: dataDir, currentIDs: make(map[uint32]bool), tracked: make(map[uint32]bool)}
	a.log.Printf("Watcher v5 started: WMI START + kernel process-handle EXIT (event driven)")
	// Every manual launch offers a choice. Scheduled login launches are silent.
	// 'Run once' disables a previously registered startup task too.
	if len(args) == 0 {
		answer := msg("DJMAX Audio Watcher를 어떻게 실행할까요?\n\n예: 로그인 시 자동 실행 유지/설정\n아니요: 이번만 실행 (자동 실행 해제)\n취소: 실행하지 않음\n\n실행 중에는 트레이 아이콘 우클릭으로도 변경할 수 있습니다.", MB_YESNOCANCEL|MB_ICONQUESTION)
		if answer == IDCANCEL || answer == 0 {
			a.log.Printf("Manual launch cancelled")
			return
		}
		enable := answer == IDYES
		if err := setStartup(enable, exe); err != nil {
			a.log.Printf("ERROR setting startup enabled=%t: %v", enable, err)
			msg("자동 실행 설정 실패 (프로그램은 실행합니다):\n"+err.Error(), MB_ICONERROR)
		} else {
			a.log.Printf("Manual launch startup preference: enabled=%t", enable)
		}
	}
	initialStartup := trayAutoStartState()
	trayStop := make(chan struct{}, 1)
	go runTray(filepath.Join(dataDir, "watcher.log"), exe, trayStop, a.log, &a.trayRunning, initialStartup)
	defer closeTray()
	// If game is running at startup, switch; if not, recover any orphaned backup.
	a.reconcile("startup")
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan wmiEvent, 128)
	a.watchCurrent(events) // also handle DJMAX already running before watcher starts
	go listenWMI(ctx, a, events)
	stop := make(chan struct{}, 1)
	go func() { procWaitForSingleObject.Call(eh, INFINITE); stop <- struct{}{} }()
	var settle *time.Timer
	var settleC <-chan time.Time
	schedule := func(d time.Duration) {
		if settle != nil {
			if !settle.Stop() {
				select {
				case <-settle.C:
				default:
				}
			}
		}
		settle = time.NewTimer(d)
		settleC = settle.C
	}
	for {
		select {
		case ev := <-events:
			switch ev.kind {
			case "READY":
				a.log.Printf("WMI listener READY")
				a.reconcile("listener ready")
				a.watchCurrent(events)
			case "START":
				a.log.Printf("WMI START received: PID=%d", ev.pid)
				a.watchPID(ev.pid, events)
				// switch48 first probes the current format, so repeated START
				// events do not trigger redundant device reconfiguration.
				a.switch48(fmt.Sprintf("WMI START PID=%d", ev.pid))
				a.currentIDs[ev.pid] = true
			case "STOP":
				if !a.tracked[ev.pid] && !a.currentIDs[ev.pid] {
					break
				}
				a.log.Printf("WMI STOP received: PID=%d", ev.pid)
				delete(a.currentIDs, ev.pid)
				schedule(350 * time.Millisecond)
			case "EXIT":
				a.log.Printf("Kernel process EXIT signaled: PID=%d", ev.pid)
				delete(a.tracked, ev.pid)
				delete(a.currentIDs, ev.pid)
				schedule(350 * time.Millisecond)
			case "WATCHFAIL", "WATCHERROR":
				a.log.Printf("Process watch issue: %s PID=%d %s", ev.kind, ev.pid, ev.info)
				schedule(350 * time.Millisecond)
			case "DISCONNECTED":
				a.log.Printf("WMI disconnected; reconciling after reconnect")
			case "INFO":
				a.log.Printf("WMI listener message: %s", ev.info)
			}
		case <-settleC:
			settleC = nil
			stillRunning := a.reconcile("post-exit settle")
			if stillRunning {
				a.watchCurrent(events)
			}
			if stillRunning {
				a.log.Printf("STOP ignored: another game process still exists")
			}
			if !stillRunning {
				if _, err := os.Stat(a.backup); err == nil && a.restoreRetries < 4 {
					a.restoreRetries++
					a.log.Printf("Restore pending; retry %d/4 after 2s", a.restoreRetries)
					schedule(2 * time.Second)
				}
			}
		case <-stop:
			a.log.Printf("Watcher stop requested by second EXE launch")
			cancel()
			if settle != nil {
				settle.Stop()
			}
			ids, err := processIDs()
			if err == nil && len(ids) == 0 {
				a.restore("watcher exit")
			} else {
				a.log.Printf("Watcher exiting with running game or snapshot error; backup retained")
			}
			a.log.Printf("Watcher exited")
			return
		case <-trayStop:
			a.log.Printf("Watcher stop requested from tray menu")
			cancel()
			if settle != nil {
				settle.Stop()
			}
			// Avoid changing the format while DJMAX is running; leave backup for recovery.
			ids, err := processIDs()
			if err == nil && len(ids) > 0 {
				a.log.Printf("Watcher exiting while DJMAX is running (PIDs=%s); keeping backup", idString(ids))
			} else if err != nil {
				a.log.Printf("ERROR snapshot on exit: %v; keeping backup", err)
			} else {
				a.restore("watcher exit")
			}
			a.log.Printf("Watcher exited")
			return
		}
	}
}
