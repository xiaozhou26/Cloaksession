//go:build windows

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestProcessStartupOmitsGUIHideInstruction(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessStartupHelper$")
	cmd.Env = append(os.Environ(), "CLOAKSESSION_TEST_STARTUP_HELPER=1")
	configureProcess(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("startup helper: %v: %s", err, out)
	}
	var startup struct {
		Flags      uint32
		ShowWindow uint16
		Console    uintptr
	}
	if err := json.Unmarshal(out, &startup); err != nil {
		t.Fatalf("decode startup info: %v: %s", err, out)
	}
	if startup.Flags&syscall.STARTF_USESHOWWINDOW != 0 && startup.ShowWindow == syscall.SW_HIDE {
		t.Error("browser inherited SW_HIDE: its initial GUI window can be hidden")
	}
	if startup.Console != 0 {
		t.Error("background helper created a console window")
	}
}

func TestHeadedBrowserWindowVisible(t *testing.T) {
	binary := os.Getenv("CLOAKSESSION_TEST_BROWSER")
	if binary == "" {
		t.Skip("set CLOAKSESSION_TEST_BROWSER for the native window test")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Window regression</title><h1>Browser window ready</h1>`)
	}))
	defer server.Close()
	m := New("", nil)
	defer m.Shutdown()
	profile := object{"id": "window", "dataDir": t.TempDir(), "startUrl": server.URL}
	settings := object{"browserEngine": "cloakbrowser", "browserBinaryPath": binary}
	info, err := m.Launch(profile, settings, "")
	if err != nil {
		t.Fatal(err)
	}
	pid := uint32(info["pid"].(int))
	user := syscall.NewLazyDLL("user32.dll")
	enumWindows := user.NewProc("EnumWindows")
	windowPID := user.NewProc("GetWindowThreadProcessId")
	isVisible := user.NewProc("IsWindowVisible")
	className := user.NewProc("GetClassNameW")
	visible := false
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var owner uint32
		windowPID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner == pid {
			var class [128]uint16
			className.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
			if syscall.UTF16ToString(class[:]) == "Chrome_WidgetWin_1" {
				if shown, _, _ := isVisible.Call(hwnd); shown != 0 {
					visible = true
				}
			}
		}
		return 1
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		enumWindows.Call(callback, 0)
		if visible {
			break
		}
		if err := pause(ctx, 50*time.Millisecond); err != nil {
			t.Fatal("browser started without a visible native window")
		}
	}
	for {
		result, err := m.Tool(ctx, "evaluate_js", object{"profileId": "window", "expression": "document.body.innerText"})
		if err == nil && obj(obj(result)["result"])["value"] == "Browser window ready" {
			break
		}
		if err := pause(ctx, 50*time.Millisecond); err != nil {
			t.Fatal("visible browser did not render the local fixture")
		}
	}
}

func TestProcessStartupHelper(t *testing.T) {
	if os.Getenv("CLOAKSESSION_TEST_STARTUP_HELPER") != "1" {
		return
	}
	kernel := syscall.NewLazyDLL("kernel32.dll")
	var startup syscall.StartupInfo
	startup.Cb = uint32(unsafe.Sizeof(startup))
	kernel.NewProc("GetStartupInfoW").Call(uintptr(unsafe.Pointer(&startup)))
	console, _, _ := kernel.NewProc("GetConsoleWindow").Call()
	fmt.Printf(`{"Flags":%d,"ShowWindow":%d,"Console":%d}`, startup.Flags, startup.ShowWindow, console)
	os.Exit(0)
}
