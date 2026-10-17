//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	wmDestroy       = 0x0002
	wmNull          = 0x0000
	wmApp           = 0x8000
	wmRButtonUp     = 0x0205
	wmLButtonDblClk = 0x0203
	nidAdd          = 0
	nidDelete       = 2
	nifMessage      = 1
	nifIcon         = 2
	nifTip          = 4
	mfString        = 0
	mfGray          = 1
	mfChecked       = 8
	mfSeparator     = 0x800
	tpmRightButton  = 2
	tpmReturnCmd    = 0x100
	menuStatus      = 1001
	menuStartup     = 1002
	menuLog         = 1003
	menuExit        = 1004
)

var (
	pRegisterClass    = user32.NewProc("RegisterClassExW")
	pCreateWindow     = user32.NewProc("CreateWindowExW")
	pDefWindowProc    = user32.NewProc("DefWindowProcW")
	pDestroyWindow    = user32.NewProc("DestroyWindow")
	pGetMessage       = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessage  = user32.NewProc("DispatchMessageW")
	pPostQuit         = user32.NewProc("PostQuitMessage")
	pPostMessage      = user32.NewProc("PostMessageW")
	pLoadIcon         = user32.NewProc("LoadIconW")
	pGetModuleHandle  = kernel32.NewProc("GetModuleHandleW")
	pShellNotify      = shell32.NewProc("Shell_NotifyIconW")
	pCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	pAppendMenu       = user32.NewProc("AppendMenuW")
	pTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	pDestroyMenu      = user32.NewProc("DestroyMenu")
	pGetCursorPos     = user32.NewProc("GetCursorPos")
	pSetForeground    = user32.NewProc("SetForegroundWindow")
)

type trayPoint struct{ x, y int32 }
type trayMsg struct {
	hwnd    uintptr
	message uint32
	_       uint32
	wparam  uintptr
	lparam  uintptr
	time    uint32
	pt      trayPoint
	private uint32
}
type trayClass struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSmall  uintptr
}
type trayNotify struct {
	size            uint32
	_               uint32
	hwnd            uintptr
	id              uint32
	flags           uint32
	callbackMessage uint32
	_               uint32
	icon            uintptr
	tip             [128]uint16
	state           uint32
	stateMask       uint32
	info            [256]uint16
	timeout         uint32
	infoTitle       [64]uint16
	infoFlags       uint32
	guid            [16]byte
	balloonIcon     uintptr
}

type trayContext struct {
	hwnd    uintptr
	logPath string
	exe     string
	stop    chan<- struct{}
	logger  *log.Logger
	running *atomic.Bool
	startup atomic.Bool
}

var trayInstance *trayContext
var trayCallback uintptr

func trayWindowProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	t := trayInstance
	if t != nil && message == wmApp+11 {
		switch uint32(lparam) {
		case wmRButtonUp:
			t.menu()
		case wmLButtonDblClk:
			t.status()
		}
		return 0
	}
	if message == wmDestroy {
		pPostQuit.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wparam, lparam)
	return r
}
func (t *trayContext) status() {
	state := "대기 중 (DJMAX가 실행되면 48kHz로 전환)"
	if t.running.Load() {
		state = "DJMAX 감지됨 (48kHz 전환 요청됨)"
	}
	auto := "꺼짐"
	if t.startup.Load() {
		auto = "켜짐"
	}
	msg("DJMAX Audio Watcher v5\n\n상태: "+state+"\n로그인 자동 실행: "+auto+"\n\n트레이 아이콘을 우클릭하면 설정 및 종료가 가능합니다.", 0)
}
func (t *trayContext) menu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	add := func(flags, id uintptr, name string) {
		pAppendMenu.Call(menu, flags, id, uintptr(unsafe.Pointer(wide(name))))
	}
	add(mfString, menuStatus, "상태 확인")
	flags := uintptr(mfString)
	if t.startup.Load() {
		flags |= mfChecked
	}
	add(flags, menuStartup, "로그인 시 자동 실행")
	add(mfString, menuLog, "로그 열기")
	add(mfSeparator, 0, "")
	add(mfString, menuExit, "종료 (오디오 설정 복구)")
	var pt trayPoint
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForeground.Call(t.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd, uintptr(int64(pt.x)), uintptr(int64(pt.y)), 0, t.hwnd, 0)
	pPostMessage.Call(t.hwnd, wmNull, 0, 0)
	switch cmd {
	case menuStatus:
		t.status()
	case menuStartup:
		go func() {
			enable := !t.startup.Load()
			if err := setStartup(enable, t.exe); err != nil {
				t.logger.Printf("ERROR tray startup setting: %v", err)
				msg("자동 실행 설정 변경 실패:\n"+err.Error(), MB_ICONERROR)
				return
			}
			t.startup.Store(enable)
			t.logger.Printf("Tray startup setting changed: enabled=%t", enable)
		}()
	case menuLog:
		p := t.logPath
		r, _, e := procShellExecute.Call(0, uintptr(unsafe.Pointer(wide("open"))), uintptr(unsafe.Pointer(wide(p))), 0, uintptr(unsafe.Pointer(wide(filepath.Dir(p)))), 1)
		if r <= 32 {
			t.logger.Printf("ERROR opening log: %v", e)
		}
	case menuExit:
		select {
		case t.stop <- struct{}{}:
		default:
		}
	}
}
func trayAutoStartState() bool {
	cmd := syscall.StringToUTF16Ptr // keep syscall dependency on windows
	_ = cmd
	// Query only once at startup, not on every popup menu.
	out, err := runPowerShellOutput("$t=Get-ScheduledTask -TaskName '" + taskName + "' -ErrorAction SilentlyContinue; if($null -ne $t -and $t.State -ne 'Disabled') { 'YES' }")
	return err == nil && strings.Contains(out, "YES")
}
func runTray(logPath, exe string, stop chan<- struct{}, logger *log.Logger, running *atomic.Bool, initialStartup bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t := &trayContext{logPath: logPath, exe: exe, stop: stop, logger: logger, running: running}
	t.startup.Store(initialStartup)
	trayInstance = t
	trayCallback = syscall.NewCallback(trayWindowProc)
	hinst, _, _ := pGetModuleHandle.Call(0)
	icon, _, _ := pLoadIcon.Call(0, 32516) // IDI_INFORMATION: built-in Windows icon
	className := wide("DJMAXAudioWatcherV5TrayWindow")
	wc := trayClass{size: uint32(unsafe.Sizeof(trayClass{})), wndProc: trayCallback, instance: hinst, icon: icon, className: className}
	atom, _, err := pRegisterClass.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		logger.Printf("ERROR RegisterClassExW tray: %v", err)
		return
	}
	hwnd, _, err := pCreateWindow.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(wide("DJMAX Audio Watcher"))), 0, 0, 0, 0, 0, 0, 0, hinst, 0)
	if hwnd == 0 {
		logger.Printf("ERROR CreateWindowExW tray: %v", err)
		return
	}
	t.hwnd = hwnd
	var n trayNotify
	n.size = uint32(unsafe.Sizeof(n))
	n.hwnd = hwnd
	n.id = 1
	n.flags = nifMessage | nifIcon | nifTip
	n.callbackMessage = wmApp + 11
	n.icon = icon
	copy(n.tip[:], syscall.StringToUTF16("DJMAX Audio Watcher v5 - 우클릭: 설정 / 종료"))
	ok, _, err := pShellNotify.Call(nidAdd, uintptr(unsafe.Pointer(&n)))
	if ok == 0 {
		logger.Printf("ERROR Shell_NotifyIconW NIM_ADD: %v", err)
	} else {
		logger.Printf("Tray icon created")
	}
	defer func() {
		pShellNotify.Call(nidDelete, uintptr(unsafe.Pointer(&n)))
		pDestroyWindow.Call(hwnd)
		trayInstance = nil
	}()
	var m trayMsg
	for {
		r, _, e := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			if int32(r) < 0 {
				logger.Printf("Tray message loop error: %v", e)
			}
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// Called on exit to close the tray's message loop cleanly.
func closeTray() {
	if t := trayInstance; t != nil && t.hwnd != 0 {
		pPostMessage.Call(t.hwnd, wmDestroy, 0, 0)
	}
}

var _ = fmt.Sprintf
var _ = os.ErrNotExist

func runPowerShellOutput(script string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
