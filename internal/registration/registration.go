package registration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// InstanceInfo describes a running logpond instance.
type InstanceInfo struct {
	Name      string    `json:"name"`
	Port      int       `json:"port"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

var (
	dirOnce    sync.Once
	dirPath    string
	dirError   error
	dirOverride string // set by tests only
)

func dir() (string, error) {
	if dirOverride != "" {
		return dirOverride, os.MkdirAll(dirOverride, 0755)
	}
	dirOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			dirError = err
			return
		}
		dirPath = filepath.Join(home, ".logpond")
		dirError = os.MkdirAll(dirPath, 0755)
	})
	return dirPath, dirError
}

// Register writes an instance registration file to ~/.logpond/<name>-<pid>.json.
func Register(name string, port int) error {
	d, err := dir()
	if err != nil {
		return err
	}
	pid := os.Getpid()
	info := InstanceInfo{
		Name:      name,
		Port:      port,
		PID:       pid,
		StartedAt: time.Now(),
	}
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(d, regFilename(name, pid)), b, 0644)
}

// Deregister removes the instance registration file for the current process.
func Deregister(name string) {
	DeregisterPID(name, os.Getpid())
}

// DeregisterPID removes the registration file for a specific name+pid.
func DeregisterPID(name string, pid int) {
	d, err := dir()
	if err != nil {
		return
	}
	os.Remove(filepath.Join(d, regFilename(name, pid)))
}

func regFilename(name string, pid int) string {
	return fmt.Sprintf("%s-%d.json", name, pid)
}

// Discover reads all registration files from ~/.logpond/.
func Discover() ([]InstanceInfo, error) {
	d, err := dir()
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(d, "*.json"))
	if err != nil {
		return nil, err
	}
	var instances []InstanceInfo
	for _, path := range matches {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var info InstanceInfo
		if err := json.Unmarshal(b, &info); err != nil {
			continue
		}
		instances = append(instances, info)
	}
	return instances, nil
}

// IsAlive checks if a process with the given PID exists.
func IsAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
