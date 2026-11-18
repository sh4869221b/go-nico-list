package cmd

import "golang.org/x/sys/unix"

// Darwin's poll is backed by kqueue, which can miss the last writer closing
// a FIFO. select observes EOF, within the descriptor range its fd_set supports.
func canWaitForFIFO(fd uintptr) bool { return fd < unix.FD_SETSIZE }

func waitForFIFO(fd uintptr) (bool, error) {
	var readable unix.FdSet
	readable.Set(int(fd))
	timeout := unix.NsecToTimeval(int64(fifoWait))
	count, err := unix.Select(int(fd)+1, &readable, nil, nil, &timeout)
	return count > 0, err
}
