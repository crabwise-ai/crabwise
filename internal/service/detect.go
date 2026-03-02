package service

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// DetectManager returns the platform-appropriate Manager, or nil if unsupported.
func DetectManager() Manager {
	return detectManagerForOS(runtime.GOOS)
}

func detectManagerForOS(goos string) Manager {
	switch goos {
	case "linux":
		return NewSystemdManager()
	case "darwin":
		return NewLaunchdManager()
	default:
		return nil
	}
}

// ValidatePrivileges checks whether the current uid/scope combination is allowed.
func ValidatePrivileges(scope Scope, uid int, sudoUser string) error {
	switch scope {
	case ScopeSystem:
		if uid != 0 {
			return fmt.Errorf("system scope requires root; run with sudo")
		}
	case ScopeUser:
		if uid == 0 {
			if sudoUser != "" {
				return fmt.Errorf(
					"user scope requires a non-root user; "+
						"run without sudo as %q instead", sudoUser,
				)
			}
			return fmt.Errorf(
				"user scope requires a non-root user; " +
					"run as the owning user, not as root",
			)
		}
	}
	return nil
}

// SuggestElevatedCommand returns a sudo-prefixed version of the given args.
func SuggestElevatedCommand(args []string) string {
	out := "sudo"
	for _, a := range args {
		out += " " + a
	}
	return out
}

func defaultRunCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func defaultGetOutput(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}
