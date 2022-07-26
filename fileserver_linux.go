package razvhost

import (
	"os"
	"syscall"
	"time"
)

func getCreationTime(fi os.FileInfo) time.Time {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		return time.Unix(int64(stat.Ctim.Sec), int64(stat.Ctim.Nsec))
	}
	return fi.ModTime()
}
