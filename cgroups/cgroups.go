package cgroups

import (
	"fmt"

	cgroup2 "github.com/containerd/cgroups/v3/cgroup2"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// CreateCgroup sets up a new cgroup with specified memory and CPU limits
func CreateCgroup(name string, pid int) error {
	// Define the cgroup path
	path := "/" + name

	// Define resource limits using specs.LinuxResources
	resources := &specs.LinuxResources{
		CPU: &specs.LinuxCPU{
			// Set CPU quota and period
			Quota:  func() *int64 { v := int64(100000); return &v }(),
			Period: func() *uint64 { v := uint64(1000000); return &v }(),
		},
		Memory: &specs.LinuxMemory{
			Limit: func() *int64 {
				var limit int64 = 100 * 1024 * 1024 // 100MB
				return &limit
			}(),
		},
	}

	// Convert specs.LinuxResources to cgroup2.Resources
	cgroupResources := cgroup2.ToResources(resources)

	// Create the cgroup manager
	mgr, err := cgroup2.NewManager("/sys/fs/cgroup", path, cgroupResources)
	if err != nil {
		return fmt.Errorf("failed to create cgroup manager: %v", err)
	}

	// Add the process to the cgroup
	if err := mgr.AddProc(uint64(pid)); err != nil {
		return fmt.Errorf("failed to add process to cgroup: %v", err)
	}

	return nil
}

func RemoveCgroup(id string) error {
	// Define the cgroup path
	path := "/" + id

	// Create the cgroup manager
	mgr, err := cgroup2.NewManager("/sys/fs/cgroup", path, nil)
	if err != nil {
		return fmt.Errorf("failed to create cgroup manager: %v", err)
	}

	// Remove the cgroup
	if err := mgr.Delete(); err != nil {
		return fmt.Errorf("failed to delete cgroup: %v", err)
	}

	return nil
}
