package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func checkLogFIFOPeer(fd int) error {
	peer, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return err
	}
	if int(peer.Uid) != os.Geteuid() || peer.Ngroups == 0 || int(peer.Groups[0]) != os.Getegid() {
		return os.ErrPermission
	}
	return nil
}
