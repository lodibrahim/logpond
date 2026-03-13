package registration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupTestDir(t *testing.T) {
	t.Helper()
	dirOverride = t.TempDir()
	t.Cleanup(func() { dirOverride = "" })
}

func TestRegisterCreatesFile(t *testing.T) {
	setupTestDir(t)

	if err := Register("test-app", 9876); err != nil {
		t.Fatalf("Register: %v", err)
	}

	pid := os.Getpid()
	path := filepath.Join(dirOverride, regFilename("test-app", pid))
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registration file: %v", err)
	}

	var info InstanceInfo
	if err := json.Unmarshal(b, &info); err != nil {
		t.Fatalf("parse registration: %v", err)
	}

	if info.Name != "test-app" {
		t.Errorf("Name = %q, want %q", info.Name, "test-app")
	}
	if info.Port != 9876 {
		t.Errorf("Port = %d, want %d", info.Port, 9876)
	}
	if info.PID != pid {
		t.Errorf("PID = %d, want %d", info.PID, pid)
	}
}

func TestDeregisterRemovesFile(t *testing.T) {
	setupTestDir(t)

	if err := Register("test-app", 9876); err != nil {
		t.Fatalf("Register: %v", err)
	}

	Deregister("test-app")

	pid := os.Getpid()
	path := filepath.Join(dirOverride, regFilename("test-app", pid))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after Deregister")
	}
}

func TestDeregisterPIDRemovesSpecificFile(t *testing.T) {
	setupTestDir(t)

	// Write a fake registration for a different PID
	info := InstanceInfo{Name: "app", Port: 9876, PID: 99999}
	b, _ := json.MarshalIndent(info, "", "  ")
	path := filepath.Join(dirOverride, regFilename("app", 99999))
	os.WriteFile(path, b, 0644)

	DeregisterPID("app", 99999)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after DeregisterPID")
	}
}

func TestDiscoverFindsRegisteredInstances(t *testing.T) {
	setupTestDir(t)

	if err := Register("app-a", 9876); err != nil {
		t.Fatalf("Register app-a: %v", err)
	}

	// Write a second fake instance
	info := InstanceInfo{Name: "app-b", Port: 9877, PID: os.Getpid()}
	b, _ := json.MarshalIndent(info, "", "  ")
	os.WriteFile(filepath.Join(dirOverride, regFilename("app-b", os.Getpid())), b, 0644)

	instances, err := Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(instances) != 2 {
		t.Fatalf("Discover returned %d instances, want 2", len(instances))
	}

	names := map[string]bool{}
	for _, inst := range instances {
		names[inst.Name] = true
	}
	if !names["app-a"] || !names["app-b"] {
		t.Errorf("expected app-a and app-b, got %v", names)
	}
}

func TestDiscoverEmptyDir(t *testing.T) {
	setupTestDir(t)

	instances, err := Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(instances) != 0 {
		t.Errorf("Discover returned %d instances, want 0", len(instances))
	}
}

func TestDiscoverSkipsInvalidJSON(t *testing.T) {
	setupTestDir(t)

	// Write a valid one
	if err := Register("valid", 9876); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Write an invalid JSON file
	os.WriteFile(filepath.Join(dirOverride, "garbage-0.json"), []byte("not json"), 0644)

	instances, err := Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(instances) != 1 {
		t.Errorf("Discover returned %d instances, want 1 (skip invalid)", len(instances))
	}
}

func TestNameCollisionUsesPID(t *testing.T) {
	setupTestDir(t)

	// Two instances with the same name but different PIDs
	info1 := InstanceInfo{Name: "app", Port: 9876, PID: 111}
	info2 := InstanceInfo{Name: "app", Port: 9877, PID: 222}

	b1, _ := json.MarshalIndent(info1, "", "  ")
	b2, _ := json.MarshalIndent(info2, "", "  ")
	os.WriteFile(filepath.Join(dirOverride, regFilename("app", 111)), b1, 0644)
	os.WriteFile(filepath.Join(dirOverride, regFilename("app", 222)), b2, 0644)

	instances, err := Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("Discover returned %d instances, want 2", len(instances))
	}

	// Deregister one, other survives
	DeregisterPID("app", 111)
	instances, _ = Discover()
	if len(instances) != 1 {
		t.Fatalf("after deregister, got %d instances, want 1", len(instances))
	}
	if instances[0].PID != 222 {
		t.Errorf("wrong instance survived: PID=%d, want 222", instances[0].PID)
	}
}

func TestIsAliveCurrentProcess(t *testing.T) {
	if !IsAlive(os.Getpid()) {
		t.Error("IsAlive(self) = false, want true")
	}
}

func TestIsAliveDeadPID(t *testing.T) {
	// PID 2147483647 is unlikely to exist
	if IsAlive(2147483647) {
		t.Error("IsAlive(2147483647) = true, want false")
	}
}

func TestRegFilename(t *testing.T) {
	got := regFilename("my-app", 12345)
	want := "my-app-12345.json"
	if got != want {
		t.Errorf("regFilename = %q, want %q", got, want)
	}
}
