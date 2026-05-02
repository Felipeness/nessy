// Package sysutil tem helpers cross-platform que abstraem chamadas pra
// shells/utilitários nativos (notification, abrir arquivo no GUI, etc).
//
// Sempre falha silenciosamente — esses helpers são "best effort".
package sysutil

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Notify dispara notificação desktop nativa.
//   macOS:   osascript display notification
//   Linux:   notify-send (libnotify, parte do Gnome/KDE base)
//   Windows: PowerShell ToastNotification (built-in)
//
// Retorna error se nenhum método funcionou — caller decide se loga.
func Notify(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		return notifyDarwin(title, body)
	case "linux":
		return notifyLinux(title, body)
	case "windows":
		return notifyWindows(title, body)
	default:
		return fmt.Errorf("notify unsupported on %s", runtime.GOOS)
	}
}

func notifyDarwin(title, body string) error {
	t := strings.ReplaceAll(title, `"`, `\"`)
	b := strings.ReplaceAll(body, `"`, `\"`)
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, b, t)
	return exec.Command("osascript", "-e", script).Run()
}

func notifyLinux(title, body string) error {
	// notify-send é universal em distros com Gnome/KDE/XFCE/etc
	if _, err := exec.LookPath("notify-send"); err != nil {
		return fmt.Errorf("notify-send not found (install libnotify-bin)")
	}
	return exec.Command("notify-send", title, body).Run()
}

func notifyWindows(title, body string) error {
	// PowerShell New-BurntToastNotification não é built-in; usamos
	// MessageBox via wscript fallback
	t := strings.ReplaceAll(title, `"`, `'`)
	b := strings.ReplaceAll(body, `"`, `'`)
	// Toast nativo via PowerShell + Windows.UI.Notifications API
	script := fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType=WindowsRuntime] > $null
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$xml.GetElementsByTagName('text')[0].AppendChild($xml.CreateTextNode("%s")) > $null
$xml.GetElementsByTagName('text')[1].AppendChild($xml.CreateTextNode("%s")) > $null
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Nessy").Show($toast)
`, t, b)
	return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
}

// Clipboard copia texto pro clipboard do SO. Best-effort.
//
//	macOS:   pbcopy
//	Linux:   xclip (X11) ou wl-copy (Wayland)
//	Windows: clip
func Clipboard(text string) error {
	switch runtime.GOOS {
	case "darwin":
		return clipboardWith("pbcopy", text)
	case "linux":
		// Prefere wl-copy (Wayland) se disponível, senão xclip
		if _, err := exec.LookPath("wl-copy"); err == nil {
			return clipboardWith("wl-copy", text)
		}
		return clipboardWith("xclip", text, "-selection", "clipboard")
	case "windows":
		return clipboardWith("clip", text)
	}
	return fmt.Errorf("clipboard unsupported on %s", runtime.GOOS)
}

func clipboardWith(bin string, text string, extraArgs ...string) error {
	cmd := exec.Command(bin, extraArgs...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// OpenPath abre arquivo/diretório/URL no aplicativo default do SO.
//   macOS:   open
//   Linux:   xdg-open
//   Windows: rundll32 url.dll,FileProtocolHandler (também serve pra paths)
func OpenPath(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "linux":
		return exec.Command("xdg-open", path).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	default:
		return fmt.Errorf("OpenPath unsupported on %s", runtime.GOOS)
	}
}
