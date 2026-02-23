//go:build linux
// +build linux

package ipc

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func (s *Server) verifyPeer(conn net.Conn) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("not a unix connection")
	}

	raw, err := uc.SyscallConn()
	if err != nil {
		return fmt.Errorf("syscall conn: %w", err)
	}

	var cred *unix.Ucred
	var credErr error
	err = raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil {
		return fmt.Errorf("control: %w", err)
	}
	if credErr != nil {
		return fmt.Errorf("getsockopt: %w", credErr)
	}

	myUID := uint32(os.Getuid())
	if cred.Uid != myUID {
		return fmt.Errorf("UID mismatch: peer=%d daemon=%d", cred.Uid, myUID)
	}

	return nil
}
