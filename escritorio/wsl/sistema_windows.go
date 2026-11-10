package wsl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Sistema is the Ejecutor of this computer: wsl.exe and the rest, without
// a console window.
func Sistema() Ejecutor { return sistema{} }

type sistema struct{}

// oculto keeps the program from opening a console, and asks wsl.exe for
// UTF-8 (the versions that can run the distribution honour it).
func oculto(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
}

func (sistema) Ejecutar(ctx context.Context, entrada []byte, nombre string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, nombre, args...)
	oculto(cmd)
	if entrada != nil {
		cmd.Stdin = bytes.NewReader(entrada)
	}
	// A killed wsl.exe can leave a helper holding its output open.
	cmd.WaitDelay = 3 * time.Second
	return cmd.CombinedOutput()
}

// Iniciar's program lives until detener, not until ctx ends.
func (sistema) Iniciar(_ context.Context, nombre string, args ...string) (func(), error) {
	cmd := exec.Command(nombre, args...)
	oculto(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	fin := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(fin)
	}()
	var una sync.Once
	return func() {
		una.Do(func() {
			_ = cmd.Process.Kill()
			<-fin
		})
	}, nil
}

// SHELLEXECUTEINFOW, and the two flags of its fMask used here.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.HWND
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIcon        windows.Handle
	hProcess     windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040 // give back the process, to wait for it
	seeMaskNoAsync        = 0x00000100 // this thread has no message loop
)

var shellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// Elevar shows the program's window: an installer that takes minutes
// tells the person what it is doing, and one that asked something could
// never be answered hidden.
func (sistema) Elevar(ctx context.Context, nombre string, args ...string) error {
	verbo, _ := windows.UTF16PtrFromString("runas")
	archivo, err := windows.UTF16PtrFromString(nombre)
	if err != nil {
		return err
	}
	parametros, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return err
	}
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verbo,
		lpFile:       archivo,
		lpParameters: parametros,
		nShow:        windows.SW_SHOWNORMAL,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if ok, _, err := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return &Problema{Clave: Cancelado, Detalle: "the administrator question was answered with no"}
		}
		return fmt.Errorf("%s as administrator: %w", nombre, err)
	}
	if info.hProcess == 0 {
		return nil
	}
	defer windows.CloseHandle(info.hProcess)
	for {
		s, err := windows.WaitForSingleObject(info.hProcess, 500)
		if err != nil {
			return err
		}
		if s == windows.WAIT_OBJECT_0 {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err() // it goes on: an elevated program is not ours to end
		}
	}
	var codigo uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &codigo); err != nil {
		return err
	}
	if codigo != 0 {
		return fmt.Errorf("%s as administrator: exit status %d", nombre, codigo)
	}
	return nil
}
