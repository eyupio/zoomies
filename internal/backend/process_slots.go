package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ProcessAdmission is a host-local policy, read before registration. A separate
// supervisor can lower capacity for host policy without
// killing a job already running. A missing policy fails closed.
type ProcessAdmission struct {
	ExpiresAt float64           `json:"expires_at"`
	Directory string            `json:"directory"`
	Capacity  int               `json:"capacity"`
	Env       map[string]string `json:"env"`
}

type processLease struct {
	Path   string `json:"path"`
	Runner string `json:"runner"`
	Slot   int    `json:"slot"`
}

func (b *ProcessBackend) reserveSlot(dir string, spec *Spec) (*processLease, error) {
	if b.admissionFile == "" {
		return nil, nil
	}
	data, err := os.ReadFile(b.admissionFile)
	if err != nil {
		return nil, fmt.Errorf("process admission: %w", err)
	}
	var policy ProcessAdmission
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, err
	}
	if policy.ExpiresAt > 0 && float64(time.Now().Unix()) >= policy.ExpiresAt {
		return nil, busyErr(fmt.Errorf("native admission policy expired"))
	}
	if !filepath.IsAbs(policy.Directory) || policy.Capacity < 0 || policy.Capacity > 64 {
		return nil, fmt.Errorf("process admission needs an absolute slot directory and capacity between 0 and 64")
	}
	if err := os.MkdirAll(policy.Directory, 0700); err != nil {
		return nil, err
	}
	unlock, err := lockProcessSlots(policy.Directory)
	if err != nil {
		return nil, err
	}
	defer unlock()
	entries, err := os.ReadDir(policy.Directory)
	if err != nil {
		return nil, err
	}
	occupied := 0
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err == nil && entry.IsDir() {
			occupied++
		}
	}
	// Lowering the limit must not admit a replacement while a higher-numbered
	// slot is still running. Existing jobs drain without being interrupted.
	if occupied >= policy.Capacity {
		return nil, busyErr(fmt.Errorf("native capacity is occupied"))
	}
	for slot := 1; slot <= policy.Capacity; slot++ {
		path := filepath.Join(policy.Directory, strconv.Itoa(slot))
		if err := os.Mkdir(path, 0700); err != nil {
			if os.IsExist(err) {
				continue
			}
			return nil, err
		}
		lease := &processLease{Path: path, Runner: dir, Slot: slot}
		// Existing ci lane owners use this same directory and PID file. Never
		// reclaim somebody else's reservation: an empty owner is still occupied.
		if err := os.WriteFile(filepath.Join(path, "runner"), []byte(dir), 0600); err != nil {
			os.Remove(path)
			return nil, err
		}
		if err := lease.bind(os.Getpid()); err != nil {
			lease.releaseLocked()
			return nil, err
		}
		data, _ := json.Marshal(lease)
		if err := os.WriteFile(filepath.Join(dir, ".zoomies-slot"), data, 0600); err != nil {
			lease.releaseLocked()
			return nil, err
		}
		env := make(map[string]string, len(spec.Env)+len(policy.Env)+1)
		for k, v := range spec.Env {
			env[k] = v
		}
		for k, v := range policy.Env {
			env[k] = strings.NewReplacer("{slot}", strconv.Itoa(slot), "{job}", dir).Replace(v)
		}
		env["CI_SLOT"] = strconv.Itoa(slot)
		if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0700); err != nil {
			lease.releaseLocked()
			return nil, err
		}
		spec.Env = env
		return lease, nil
	}
	return nil, busyErr(fmt.Errorf("all native host slots are occupied or admission is paused"))
}

func (l *processLease) bind(pid int) error {
	if l == nil {
		return nil
	}
	return os.WriteFile(filepath.Join(l.Path, "owner"), []byte(strconv.Itoa(pid)), 0600)
}
func (l *processLease) release() {
	if l == nil {
		return
	}
	unlock, err := lockProcessSlots(filepath.Dir(l.Path))
	if err != nil {
		return
	}
	defer unlock()
	l.releaseLocked()
}

// Reservation error paths already hold the admission lock.
func (l *processLease) releaseLocked() {
	if l == nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(l.Path, "runner"))
	if err == nil && string(data) == l.Runner {
		_ = os.RemoveAll(l.Path)
	}
}
func readProcessLease(dir string) *processLease {
	data, err := os.ReadFile(filepath.Join(dir, ".zoomies-slot"))
	if err != nil {
		return nil
	}
	var l processLease
	if json.Unmarshal(data, &l) != nil || l.Runner != dir {
		return nil
	}
	return &l
}

// A failed RemoveAll may already have deleted .zoomies-slot. The slot's own
// runner record survives that operation and lets a later removal retry release it.
func (b *ProcessBackend) processLease(dir string) *processLease {
	if lease := readProcessLease(dir); lease != nil {
		return lease
	}
	data, err := os.ReadFile(b.admissionFile)
	if err != nil {
		return nil
	}
	var policy ProcessAdmission
	if json.Unmarshal(data, &policy) != nil || !filepath.IsAbs(policy.Directory) {
		return nil
	}
	entries, err := os.ReadDir(policy.Directory)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		slot, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		path := filepath.Join(policy.Directory, entry.Name())
		runner, err := os.ReadFile(filepath.Join(path, "runner"))
		if err == nil && string(runner) == dir {
			return &processLease{Path: path, Runner: dir, Slot: slot}
		}
	}
	return nil
}

func (l *processLease) ownerAlive() bool {
	if l == nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(l.Path, "owner"))
	if err != nil {
		return true
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return true
	}
	return processAlive(pid) || runnerGroupAlive(pid)
}
