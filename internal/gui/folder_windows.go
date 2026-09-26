package gui

import (
	"fmt"
	"os"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/yetone/magpie/internal/proc"
)

// openFolder starts Explorer on the folder and leaves it to open: not on the
// window's thread, and not waited for, where Wails' own way holds the thread
// the windows are drawn on until Explorer is done and kills it after ten
// seconds.
func openFolder(_ *application.App, path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	cmd := proc.Command("explorer.exe")
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// Explorer reads its own command line: the path quoted, not escaped as Go would
	cmd.SysProcAttr.CmdLine = fmt.Sprintf(`explorer.exe "%s"`, path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("explorer: %w", err)
	}
	go cmd.Wait() // it exits 1 when it did open the folder
	return nil
}
