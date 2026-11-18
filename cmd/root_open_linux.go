package cmd

import (
	"os"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

func canWaitForFIFO(uintptr) bool { return true }

func waitForFIFO(fd uintptr) (bool, error) {
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP}}
	count, err := unix.Poll(poll, int(fifoWait/time.Millisecond))
	return count > 0, err
}

// Pin the inode so a rename or unlink does not prevent waking the writer.
// Reopening it for read is needed only for cancellation.
func pinLogFIFO(path string, info os.FileInfo) (*os.File, string) {
	anchor, err := os.OpenFile(path, unix.O_PATH, 0)
	if err != nil {
		return nil, ""
	}
	pinned := "/proc/self/fd/" + strconv.FormatUint(uint64(anchor.Fd()), 10)
	pinnedInfo, _ := os.Stat(pinned)
	if !sameFile(info, pinnedInfo) || unix.Faccessat(unix.AT_FDCWD, pinned, unix.R_OK, unix.AT_EACCESS) != nil {
		_ = anchor.Close()
		return nil, ""
	}
	return anchor, pinned
}
