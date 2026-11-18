package cmd

import (
	"time"

	"golang.org/x/sys/unix"
)

func canWaitForFIFO(uintptr) bool { return true }

func waitForFIFO(fd uintptr) (bool, error) {
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP}}
	count, err := unix.Poll(poll, int(fifoWait/time.Millisecond))
	return count > 0, err
}
