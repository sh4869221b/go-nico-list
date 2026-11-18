package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func checkLogFIFOPeer(fd int) error {
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return err
	}
	if int(peer.Pid) != os.Getppid() || int(peer.Uid) != os.Geteuid() || int(peer.Gid) != os.Getegid() {
		return os.ErrPermission
	}
	return nil
}
