package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectManagerForOS_Linux(t *testing.T) {
	mgr := detectManagerForOS("linux")
	require.IsType(t, &SystemdManager{}, mgr)
}

func TestDetectManagerForOS_Darwin(t *testing.T) {
	mgr := detectManagerForOS("darwin")
	require.IsType(t, &LaunchdManager{}, mgr)
}

func TestDetectManagerForOS_Unknown(t *testing.T) {
	mgr := detectManagerForOS("windows")
	require.Nil(t, mgr)
}

func TestValidatePrivileges_SystemRequiresRoot(t *testing.T) {
	err := ValidatePrivileges(ScopeSystem, 1000, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "system scope requires root")
}

func TestValidatePrivileges_SystemAllowsRoot(t *testing.T) {
	err := ValidatePrivileges(ScopeSystem, 0, "")
	require.NoError(t, err)
}

func TestValidatePrivileges_UserAllowsNonRoot(t *testing.T) {
	err := ValidatePrivileges(ScopeUser, 1000, "")
	require.NoError(t, err)
}

func TestValidatePrivileges_UserRejectsSudo(t *testing.T) {
	err := ValidatePrivileges(ScopeUser, 0, "alice")
	require.Error(t, err)
	require.Contains(t, err.Error(), "user scope requires a non-root user")
}

func TestValidatePrivileges_UserRejectsDirectRoot(t *testing.T) {
	err := ValidatePrivileges(ScopeUser, 0, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "user scope requires a non-root user")
}

func TestSuggestElevatedCommand(t *testing.T) {
	got := SuggestElevatedCommand([]string{"crabwise", "service", "inject", "--agent", "openclaw"})
	require.Equal(t, "sudo crabwise service inject --agent openclaw", got)
}
