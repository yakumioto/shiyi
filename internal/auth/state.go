package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"os"
	"syscall"
)

// State is separate from memory. Only credential digests, CSRF secrets and
// lifecycle timestamps are retained; no password or business input is stored.
type diskState struct {
	Version          int                     `json:"version"`
	CredentialConfig string                  `json:"credential_config"`
	Key              []byte                  `json:"key"`
	Sessions         map[string]loginSession `json:"sessions"`
}

const stateFile = "state.json"
const maxSessions = 4096
const maxStateBytes = 4 << 20

func safeInfo(info os.FileInfo, mode os.FileMode, directory bool) bool {
	if info.Mode().Perm() != mode || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
func (m *module) openState() error {
	if err := os.MkdirAll(m.cfg.StateDir, 0700); err != nil {
		return ErrUnavailable
	}
	info, err := os.Lstat(m.cfg.StateDir)
	if err != nil || !safeInfo(info, 0700, true) {
		return ErrUnavailable
	}
	root, err := os.OpenRoot(m.cfg.StateDir)
	if err != nil {
		return ErrUnavailable
	}
	m.root = root
	lock, err := root.OpenFile("lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return ErrUnavailable
	}
	m.lock = lock
	info, err = lock.Stat()
	if err != nil || !safeInfo(info, 0600, false) {
		return ErrUnavailable
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return ErrUnavailable
	}
	info, err = root.Lstat(stateFile)
	if os.IsNotExist(err) {
		return m.commit(m.sessions)
	}
	if err != nil || !safeInfo(info, 0600, false) || info.Size() > maxStateBytes {
		return ErrUnavailable
	}
	f, err := root.Open(stateFile)
	if err != nil {
		return ErrUnavailable
	}
	defer f.Close()
	var disk diskState
	dec := json.NewDecoder(io.LimitReader(f, maxStateBytes+1))
	dec.DisallowUnknownFields()
	if dec.Decode(&disk) != nil || dec.Decode(new(any)) != io.EOF || disk.Version != 1 || len(disk.Key) != 32 || !encodedSecret(disk.CredentialConfig) || disk.Sessions == nil || len(disk.Sessions) > maxSessions {
		return ErrUnavailable
	}
	for id, s := range disk.Sessions {
		if !encodedSecret(id) || !encodedSecret(s.CSRF) || s.Created.IsZero() || s.LastUsed.Before(s.Created) {
			return ErrUnavailable
		}
	}
	m.key = disk.Key
	m.sessions = disk.Sessions
	if disk.CredentialConfig != digest(m.cfg.PasswordHash) {
		m.key = make([]byte, 32)
		_, _ = rand.Read(m.key)
		return m.commit(make(map[string]loginSession))
	}
	return nil
}
func encodedSecret(s string) bool {
	v, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(v) == 32 && base64.RawURLEncoding.EncodeToString(v) == s
}
func (m *module) closeState() error {
	var failed bool
	if m.lock != nil {
		if err := m.lock.Close(); err != nil {
			failed = true
		}
		m.lock = nil
	}
	if m.root != nil {
		if err := m.root.Close(); err != nil {
			failed = true
		}
		m.root = nil
	}
	if failed {
		return ErrUnavailable
	}
	return nil
}
func (m *module) stateSafe() bool {
	if m.root == nil {
		return m.cfg.DevAnonymous
	}
	info, err := m.root.Stat(".")
	if err != nil || !safeInfo(info, 0700, true) {
		return false
	}
	info, err = m.root.Lstat(stateFile)
	return err == nil && safeInfo(info, 0600, false)
}

// commit publishes a replacement only after file and directory fsync. A failure
// poisons this instance: a rename might already be visible but not yet durable,
// so neither old nor tentative in-memory authorization may be used afterward.
func (m *module) commit(sessions map[string]loginSession) error {
	if m.cfg.DevAnonymous {
		return nil
	}
	if m.failed || m.root == nil {
		return ErrUnavailable
	}
	info, err := m.root.Stat(".")
	if err != nil || !safeInfo(info, 0700, true) {
		m.failed = true
		return ErrUnavailable
	}
	if info, err := m.root.Lstat(stateFile); !os.IsNotExist(err) && (err != nil || !safeInfo(info, 0600, false)) {
		m.failed = true
		return ErrUnavailable
	}
	next := maps.Clone(sessions)
	now := m.now()
	for id, s := range next {
		if !now.Before(s.LastUsed.Add(SessionIdleTimeout)) || !now.Before(s.Created.Add(SessionAbsoluteTTL)) {
			delete(next, id)
		}
	}
	if len(next) > maxSessions {
		return ErrUnavailable
	}
	raw, err := json.Marshal(diskState{Version: 1, CredentialConfig: digest(m.cfg.PasswordHash), Key: m.key, Sessions: next})
	if err != nil {
		return ErrUnavailable
	}
	name := ".state-" + secret()
	f, err := m.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		m.failed = true
		return ErrUnavailable
	}
	defer m.root.Remove(name)
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = m.root.Rename(name, stateFile)
	}
	if err == nil {
		var directory *os.File
		directory, err = m.root.Open(".")
		if err == nil {
			err = directory.Sync()
			_ = directory.Close()
		}
	}
	if err != nil {
		m.failed = true
		return ErrUnavailable
	}
	m.sessions = next
	return nil
}
