package razvhost

import (
	"os"
	"syscall"
	"time"
)

func getCreationTime(fi os.FileInfo) time.Time {
	d := fi.Sys().(*syscall.Win32FileAttributeData)
	return time.Unix(0, d.CreationTime.Nanoseconds())
}
