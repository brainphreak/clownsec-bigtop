//go:build windows

package main

// The Windows notification-area ("system tray") icon, straight on the Win32 API: a hidden window
// receives the icon's mouse messages; left-click opens the setup page, right-click a menu.

import (
	_ "embed"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

//go:embed assets/tray32.png
var trayPNG []byte

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassExW         = user32.NewProc("RegisterClassExW")
	pCreateWindowExW          = user32.NewProc("CreateWindowExW")
	pDefWindowProcW           = user32.NewProc("DefWindowProcW")
	pGetMessageW              = user32.NewProc("GetMessageW")
	pTranslateMessage         = user32.NewProc("TranslateMessage")
	pDispatchMessageW         = user32.NewProc("DispatchMessageW")
	pPostMessageW             = user32.NewProc("PostMessageW")
	pCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	pAppendMenuW              = user32.NewProc("AppendMenuW")
	pTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	pDestroyMenu              = user32.NewProc("DestroyMenu")
	pSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	pGetCursorPos             = user32.NewProc("GetCursorPos")
	pCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	pRegisterWindowMessageW   = user32.NewProc("RegisterWindowMessageW")
	pMessageBoxW              = user32.NewProc("MessageBoxW")
	pShellNotifyIconW         = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmApp          = 0x8000
	wmTray         = wmApp + 1
	wmNull         = 0x0000
	wmCommand      = 0x0111
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
	wmContextMenu  = 0x007B
	nimAdd         = 0
	nimModify      = 1
	nimDelete      = 2
	nifMessage     = 0x1
	nifIcon        = 0x2
	nifTip         = 0x4
	nifInfo        = 0x10
	niifInfo       = 0x1
	mfString       = 0x0
	mfSeparator    = 0x800
	mfGrayed       = 0x1
	tpmReturnCmd   = 0x100
	tpmRightButton = 0x2
	tpmNoNotify    = 0x80
)

type wndClassEx struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSm                             uintptr
}

type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        [16]byte
	BalloonIcon     uintptr
}

type point struct{ X, Y int32 }

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

// TrayActions are what the menu does; main fills them in.
type TrayActions struct {
	Version     string
	OpenPage    func()
	UpdateLabel func() (label string, enabled bool) // "Check for updates" / "Update to 261115.0900"
	Update      func()                              // check, or install the waiting update
	ShowLog     func()
	OpenData    func()
	Restart     func()
	Quit        func()
}

var (
	trayMu       sync.Mutex
	trayNID      notifyIconData
	trayUp       bool
	trayAct      TrayActions
	taskbarMsg   uintptr
	trayCallback = syscall.NewCallback(trayWndProc)
)

func u16(s string, dst []uint16) {
	p, _ := syscall.UTF16FromString(s)
	n := copy(dst, p)
	if n == len(dst) {
		dst[n-1] = 0
	}
}

func trayAdd() {
	trayMu.Lock()
	defer trayMu.Unlock()
	pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&trayNID)))
	trayUp = true
}

// runTray puts the icon in the notification area and runs its message loop (blocks: call it on
// its own goroutine). The tray is the Windows face of Big Top -- there is no console window.
func runTray(a TrayActions) {
	runtime.LockOSThread() // the window and its messages belong to this thread
	trayAct = a
	inst, _, _ := pGetModuleHandleW.Call(0)
	cls, _ := syscall.UTF16PtrFromString("ClownsecBigTopTray")
	wc := wndClassEx{WndProc: trayCallback, Instance: inst, ClassName: cls}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	title, _ := syscall.UTF16PtrFromString("Clownsec Big Top")
	hwnd, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, inst, 0) // never shown
	if hwnd == 0 {
		return
	}
	tb, _ := syscall.UTF16PtrFromString("TaskbarCreated") // Explorer restarted: put the icon back
	taskbarMsg, _, _ = pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(tb)))

	icon, _, _ := pCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&trayPNG[0])), uintptr(len(trayPNG)),
		1, 0x00030000, 32, 32, 0)
	trayNID.Size = uint32(unsafe.Sizeof(trayNID))
	trayNID.Wnd = hwnd
	trayNID.ID = 1
	trayNID.Flags = nifMessage | nifIcon | nifTip
	trayNID.CallbackMessage = wmTray
	trayNID.Icon = icon
	u16("Clownsec Big Top "+a.Version+" - running", trayNID.Tip[:])
	trayAdd()

	var m winMsg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func trayWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch {
	case msg == wmTray:
		switch uint32(lparam) & 0xFFFF {
		case wmLButtonUp:
			go trayAct.OpenPage()
		case wmRButtonUp, wmContextMenu:
			trayMenu(hwnd)
		}
		return 0
	case taskbarMsg != 0 && msg == taskbarMsg:
		trayAdd()
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func trayMenu(hwnd uintptr) {
	menu, _, _ := pCreatePopupMenu.Call()
	add := func(id uintptr, text string, flags uintptr) {
		p, _ := syscall.UTF16PtrFromString(text)
		pAppendMenuW.Call(menu, mfString|flags, id, uintptr(unsafe.Pointer(p)))
	}
	sep := func() { pAppendMenuW.Call(menu, mfSeparator, 0, 0) }
	add(0, "Clownsec Big Top "+trayAct.Version, mfGrayed)
	sep()
	add(1, "Open setup page", 0)
	label, enabled := trayAct.UpdateLabel()
	var fl uintptr
	if !enabled {
		fl = mfGrayed
	}
	add(2, label, fl)
	sep()
	add(3, "Show log", 0)
	add(4, "Open data folder", 0)
	sep()
	add(6, "Restart Big Top", 0)
	add(5, "Quit Big Top", 0)
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hwnd) // or the menu won't close when clicking elsewhere
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmReturnCmd|tpmRightButton|tpmNoNotify,
		uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	pPostMessageW.Call(hwnd, wmNull, 0, 0)
	pDestroyMenu.Call(menu)
	switch cmd {
	case 1:
		go trayAct.OpenPage()
	case 2:
		go trayAct.Update()
	case 3:
		go trayAct.ShowLog()
	case 4:
		go trayAct.OpenData()
	case 5:
		go trayAct.Quit()
	case 6:
		go trayAct.Restart()
	}
}

// trayNotify shows a notification from the icon (e.g. "Big Top 261115.0900 is available").
func trayNotify(title, text string) {
	trayMu.Lock()
	defer trayMu.Unlock()
	if !trayUp {
		return
	}
	n := trayNID
	n.Flags = nifInfo
	n.InfoFlags = niifInfo
	u16(title, n.InfoTitle[:])
	u16(text, n.Info[:])
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

// trayRemove takes the icon away (on exit -- Windows leaves a dead icon behind otherwise).
func trayRemove() {
	trayMu.Lock()
	defer trayMu.Unlock()
	if trayUp {
		pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&trayNID)))
		trayUp = false
	}
}

// messageBox: a Windows dialog (a tray app has no console to print into).
func messageBox(title, text string, isError bool) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	var flags uintptr = 0x40 // MB_ICONINFORMATION
	if isError {
		flags = 0x10 // MB_ICONERROR
	}
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
}

const haveTray = true
