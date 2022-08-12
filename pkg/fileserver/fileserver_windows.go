package fileserver

import (
	"os"
	"syscall"
	"time"
)

func getCreationTime(fi os.FileInfo) time.Time {
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, d.CreationTime.Nanoseconds())
	}
	return fi.ModTime()
}
