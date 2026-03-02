package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSystemdManager_Resolve_SystemScope(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "myapp.service")
	require.NoError(t, os.WriteFile(unitPath, []byte("[Unit]\n"), 0644))

	m := &SystemdManager{SystemDirs: []string{dir}}
	res, err := m.Resolve("myapp", ScopeSystem)

	require.NoError(t, err)
	require.Equal(t, "myapp", res.ServiceName)
	require.Equal(t, ScopeSystem, res.Scope)
	require.NotNil(t, res.Systemd)
	require.Equal(t, "myapp.service", res.Systemd.UnitName)
	require.Equal(t, unitPath, res.Systemd.UnitPath)
}

func TestSystemdManager_Resolve_UserScope(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "myapp.service")
	require.NoError(t, os.WriteFile(unitPath, []byte("[Unit]\n"), 0644))

	m := &SystemdManager{UserDirs: []string{dir}}
	res, err := m.Resolve("myapp", ScopeUser)

	require.NoError(t, err)
	require.Equal(t, ScopeUser, res.Scope)
	require.Equal(t, unitPath, res.Systemd.UnitPath)
	require.Equal(t, dir, res.Systemd.DropInRoot)
}

func TestSystemdManager_Resolve_NotFound(t *testing.T) {
	dir := t.TempDir()

	m := &SystemdManager{SystemDirs: []string{dir}}
	_, err := m.Resolve("nosuch", ScopeSystem)

	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSystemdManager_Resolve_NoFallbackAcrossScopes(t *testing.T) {
	sysDir := t.TempDir()
	userDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sysDir, "myapp.service"), []byte("[Unit]\n"), 0644))

	m := &SystemdManager{SystemDirs: []string{sysDir}, UserDirs: []string{userDir}}
	_, err := m.Resolve("myapp", ScopeUser)

	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSystemdManager_Inject_WritesDropIn(t *testing.T) {
	dir := t.TempDir()
	res := Resolution{
		ServiceName: "myapp",
		Scope:       ScopeSystem,
		Systemd: &SystemdResolution{
			UnitName:   "myapp.service",
			DropInRoot: dir,
		},
	}
	cfg := EnvConfig{ProxyURL: "http://127.0.0.1:9119", CACert: "/tmp/ca.crt"}

	m := &SystemdManager{}
	result, err := m.Inject(res, cfg)

	require.NoError(t, err)
	require.True(t, result.Written)

	data, err := os.ReadFile(result.Path)
	require.NoError(t, err)

	content := string(data)
	require.True(t, strings.HasPrefix(content, dropInHeader))
	require.Contains(t, content, "[Service]")
	require.Contains(t, content, `Environment="HTTPS_PROXY=http://127.0.0.1:9119"`)
	require.Contains(t, content, `Environment="NODE_EXTRA_CA_CERTS=/tmp/ca.crt"`)
}

func TestSystemdManager_Inject_Idempotent(t *testing.T) {
	dir := t.TempDir()
	res := Resolution{
		ServiceName: "myapp",
		Scope:       ScopeSystem,
		Systemd: &SystemdResolution{
			UnitName:   "myapp.service",
			DropInRoot: dir,
		},
	}
	cfg := EnvConfig{ProxyURL: "http://127.0.0.1:9119"}

	m := &SystemdManager{}
	r1, err := m.Inject(res, cfg)
	require.NoError(t, err)
	require.True(t, r1.Written)

	r2, err := m.Inject(res, cfg)
	require.NoError(t, err)
	require.True(t, r2.Written)

	d1, _ := os.ReadFile(r1.Path)
	d2, _ := os.ReadFile(r2.Path)
	require.Equal(t, string(d1), string(d2))
}

func TestSystemdManager_Remove_DeletesDropIn(t *testing.T) {
	dir := t.TempDir()
	res := Resolution{
		ServiceName: "myapp",
		Scope:       ScopeSystem,
		Systemd: &SystemdResolution{
			UnitName:   "myapp.service",
			DropInRoot: dir,
		},
	}
	cfg := EnvConfig{ProxyURL: "http://127.0.0.1:9119"}

	m := &SystemdManager{}
	_, err := m.Inject(res, cfg)
	require.NoError(t, err)

	result, err := m.Remove(res, cfg)
	require.NoError(t, err)
	require.True(t, result.Removed)

	_, err = os.Stat(result.Path)
	require.True(t, os.IsNotExist(err))
}

func TestSystemdManager_Remove_CleansEmptyDir(t *testing.T) {
	dir := t.TempDir()
	res := Resolution{
		ServiceName: "myapp",
		Scope:       ScopeSystem,
		Systemd: &SystemdResolution{
			UnitName:   "myapp.service",
			DropInRoot: dir,
		},
	}
	cfg := EnvConfig{ProxyURL: "http://127.0.0.1:9119"}

	m := &SystemdManager{}
	_, err := m.Inject(res, cfg)
	require.NoError(t, err)

	_, err = m.Remove(res, cfg)
	require.NoError(t, err)

	dropInDir := filepath.Join(dir, "myapp.service.d")
	_, err = os.Stat(dropInDir)
	require.True(t, os.IsNotExist(err), "empty .d directory should be cleaned up")
}

func TestSystemdManager_Remove_NotInjected(t *testing.T) {
	dir := t.TempDir()
	res := Resolution{
		ServiceName: "myapp",
		Scope:       ScopeSystem,
		Systemd: &SystemdResolution{
			UnitName:   "myapp.service",
			DropInRoot: dir,
		},
	}

	m := &SystemdManager{}
	result, err := m.Remove(res, EnvConfig{})
	require.NoError(t, err)
	require.False(t, result.Removed)
}

func TestSystemdManager_CheckInjected_ValidDropIn(t *testing.T) {
	dir := t.TempDir()
	res := Resolution{
		ServiceName: "myapp",
		Scope:       ScopeSystem,
		Systemd: &SystemdResolution{
			UnitName:   "myapp.service",
			DropInRoot: dir,
		},
	}
	cfg := EnvConfig{ProxyURL: "http://127.0.0.1:9119"}

	m := &SystemdManager{}
	_, err := m.Inject(res, cfg)
	require.NoError(t, err)

	injected, err := m.CheckInjected(res)
	require.NoError(t, err)
	require.True(t, injected)
}

func TestSystemdManager_CheckInjected_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	dropInDir := filepath.Join(dir, "myapp.service.d")
	require.NoError(t, os.MkdirAll(dropInDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dropInDir, dropInFileName), []byte(""), 0644))

	res := Resolution{
		Systemd: &SystemdResolution{
			UnitName:   "myapp.service",
			DropInRoot: dir,
		},
	}

	m := &SystemdManager{}
	injected, err := m.CheckInjected(res)
	require.NoError(t, err)
	require.False(t, injected)
}

func TestSystemdManager_CheckInjected_MissingHeader(t *testing.T) {
	dir := t.TempDir()
	dropInDir := filepath.Join(dir, "myapp.service.d")
	require.NoError(t, os.MkdirAll(dropInDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dropInDir, dropInFileName),
		[]byte("[Service]\nEnvironment=\"HTTPS_PROXY=http://localhost:9119\"\n"),
		0644,
	))

	res := Resolution{
		Systemd: &SystemdResolution{
			UnitName:   "myapp.service",
			DropInRoot: dir,
		},
	}

	m := &SystemdManager{}
	injected, err := m.CheckInjected(res)
	require.NoError(t, err)
	require.False(t, injected)
}

func TestSystemdManager_CheckInjected_NoFile(t *testing.T) {
	dir := t.TempDir()
	res := Resolution{
		Systemd: &SystemdResolution{
			UnitName:   "myapp.service",
			DropInRoot: dir,
		},
	}

	m := &SystemdManager{}
	injected, err := m.CheckInjected(res)
	require.NoError(t, err)
	require.False(t, injected)
}

func TestSystemdManager_CheckInjected_ReadError(t *testing.T) {
	dir := t.TempDir()
	dropInDir := filepath.Join(dir, "myapp.service.d")
	require.NoError(t, os.MkdirAll(dropInDir, 0755))

	filePath := filepath.Join(dropInDir, dropInFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))
	require.NoError(t, os.Chmod(filePath, 0000))
	t.Cleanup(func() { os.Chmod(filePath, 0644) })

	res := Resolution{
		Systemd: &SystemdResolution{
			UnitName:   "myapp.service",
			DropInRoot: dir,
		},
	}

	m := &SystemdManager{}
	_, err := m.CheckInjected(res)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read drop-in")
}

func TestSystemdManager_Restart_SystemScope(t *testing.T) {
	var calls []string
	m := &SystemdManager{
		RunCmd: func(name string, args ...string) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		},
	}

	res := Resolution{
		ServiceName: "myapp",
		Scope:       ScopeSystem,
		Systemd:     &SystemdResolution{UnitName: "myapp.service"},
	}

	err := m.Restart(res)
	require.NoError(t, err)
	require.Equal(t, []string{
		"systemctl daemon-reload",
		"systemctl restart myapp",
	}, calls)
}

func TestSystemdManager_Restart_UserScope(t *testing.T) {
	var calls []string
	m := &SystemdManager{
		RunCmd: func(name string, args ...string) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		},
	}

	res := Resolution{
		ServiceName: "myapp",
		Scope:       ScopeUser,
		Systemd:     &SystemdResolution{UnitName: "myapp.service"},
	}

	err := m.Restart(res)
	require.NoError(t, err)
	require.Equal(t, []string{
		"systemctl --user daemon-reload",
		"systemctl --user restart myapp",
	}, calls)
}
