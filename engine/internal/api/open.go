package api

import (
	"os/exec"
	"runtime"
)

// OpenBrowser asks the desktop to open a URL. Failure is not an error worth
// stopping for: the address is printed either way, and a person can click it.
func OpenBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
