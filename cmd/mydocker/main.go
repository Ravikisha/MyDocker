package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"mydocker/cgroups"
	"mydocker/network"

	"github.com/google/uuid"
)

type stringSlice []string

func (s *stringSlice) String() string { return fmt.Sprintf("%v", *s) }
func (s *stringSlice) Set(val string) error {
	*s = append(*s, val)
	return nil
}

type PortMapping struct {
	HostPort      int `json:"hostPort"`
	ContainerPort int `json:"containerPort"`
}

type ContainerInfo struct {
	ID      string        `json:"id"`
	Image   string        `json:"image"`
	Cmd     []string      `json:"cmd"`
	Volumes []string      `json:"volumes"`
	Ports   []PortMapping `json:"ports"`
	IP      string        `json:"ip"`
	PID     int           `json:"pid"`
}

/* ─────────────────────────────  MAIN  ────────────────────────────────────── */

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: mydocker <command> [options]")
	}
	switch os.Args[1] {
	case "run":
		// Define flags for run command
		runCmd := flag.NewFlagSet("run", flag.ExitOnError)
		var volumes stringSlice
		var ports stringSlice
		runCmd.Var(&volumes, "v", "Volume mounts (host:container)")
		runCmd.Var(&ports, "p", "Port mappings (host:container)")
		runCmd.Parse(os.Args[2:]) // parse flags after "run"

		// Positional args: image and command
		args := runCmd.Args()
		if len(args) < 2 {
			log.Fatalf("Usage: mydocker run [options] <image> <command>")
		}
		image := args[0]
		cmdArgs := args[1:]

		// maps the ports
		var portMappings []PortMapping
		for _, port := range ports {
			parts := strings.Split(port, ":")
			if len(parts) != 2 {
				log.Fatalf("Invalid port mapping %s, expected host:container", port)
			}
			hostPort, err := strconv.Atoi(parts[0])
			if err != nil {
				log.Fatalf("Invalid host port %s: %v", parts[0], err)
			}
			containerPort, err := strconv.Atoi(parts[1])
			if err != nil {
				log.Fatalf("Invalid container port %s: %v", parts[1], err)
			}
			portMappings = append(portMappings, PortMapping{
				HostPort:      hostPort,
				ContainerPort: containerPort,
			})
		}

		// Generate random container ID and start container
		id := uuid.New().String()
		if _, err := startContainer(id, image, cmdArgs, volumes, portMappings); err != nil {
			log.Fatalf("Error: %v", err)
		}
		fmt.Println(id)
	case "pull":
		if len(os.Args) < 3 {
			fmt.Println("Usage: mydocker pull <image>")
			os.Exit(1)
		}
		image := os.Args[2]
		if err := pullImage(image); err != nil {
			fmt.Printf("Error pulling image %s: %v\n", image, err)
			os.Exit(1)
		}
		fmt.Printf("Image %s pulled successfully\n", image)
	case "ps":
		containers, err := listContainers()
		if err != nil {
			fmt.Printf("Error listing containers: %v\n", err)
			os.Exit(1)
		}
		if len(containers) == 0 {
			fmt.Println("No containers found")
		}

	case "stop":
		if len(os.Args) < 3 {
			fmt.Println("Usage: mydocker stop <container_id>")
			os.Exit(1)
		}
		id := os.Args[2]
		if err := stopContainer(id); err != nil {
			fmt.Printf("Error stopping container %s: %v\n", id, err)
			os.Exit(1)
		}
		fmt.Printf("Container %s stopped successfully\n", id)
	case "exec":
		if len(os.Args) < 4 {
			fmt.Println("Usage: mydocker exec <container_id> <command>")
			os.Exit(1)
		}
		containerID := os.Args[2]
		cmd := os.Args[3:]
		if err := execContainer(containerID, cmd); err != nil {
			fmt.Printf("Error executing command in container %s: %v\n", containerID, err)
			os.Exit(1)
		}
		fmt.Printf("Command executed successfully in container %s\n", containerID)
	case "images":
		images, err := listImages()
		if err != nil {
			fmt.Printf("Error listing images: %v\n", err)
			os.Exit(1)
		}
		if len(images) == 0 {
			fmt.Println("No images found")
		} else {
			fmt.Println("Available images:")
			for _, img := range images {
				fmt.Println(img)
			}
		}
	case "child":
		if len(os.Args) < 3 {
			log.Fatalf("Usage: mydocker child <container_id> <command>")
		}
		containerID := os.Args[2]
		fmt.Printf("Child process for container %s started successfully\n", containerID)
		if err := child(containerID); err != nil {
			log.Fatalf("Error in child process for container %s: %v", containerID, err)
		}
	case "version":
		fmt.Println("mydocker version 0.1.0")
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		fmt.Println("Available commands: run, pull, ps, stop")
		os.Exit(1)
	}
}

/*// ───────────────────────────  images  ────────────────────────── */
func listImages() ([]string, error) {
	imagesDir := "/var/lib/mydocker/images"
	files, err := os.ReadDir(imagesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read images directory: %v", err)
	}

	var images []string
	for _, file := range files {
		if file.IsDir() {
			images = append(images, file.Name())
		}
	}
	return images, nil
}

func startContainer(id, image string, cmd []string, volumes []string, ports []PortMapping) (int, error) {
	basePath := "/var/lib/mydocker"
	imagePath := filepath.Join(basePath, "images", image)
	containerPath := filepath.Join(basePath, "containers", id)
	bundlePath := filepath.Join(containerPath, "bundle")

	// Create bundle directory
	if err := os.MkdirAll(bundlePath, 0755); err != nil {
		return 0, fmt.Errorf("failed to create bundle directory: %v", err)
	}

	// Unpack OCI image into bundle
	umociCmd := exec.Command("umoci", "unpack", "--image", imagePath, bundlePath)
	umociCmd.Stdout = os.Stdout
	umociCmd.Stderr = os.Stderr
	if err := umociCmd.Run(); err != nil {
		return 0, fmt.Errorf("umoci unpack failed: %v", err)
	}

	// Set up cgroup for container (memory/CPU limits)
	if err := cgroups.CreateCgroup(id, os.Getpid()); err != nil {
		return 0, fmt.Errorf("failed to create cgroup: %v", err)
	}

	// Prepare command to re-exec self as child process
	exePath, err := os.Readlink("/proc/self/exe")
	if err != nil {
		exePath, err = filepath.Abs(os.Args[0])
		if err != nil {
			return 0, fmt.Errorf("failed to determine executable path: %v", err)
		}
	}

	childCmd := exec.Command(exePath, append(append([]string{"child"}, id), cmd...)...)
	// Pass volume mounts via environment
	volumesEnv := ""
	if len(volumes) > 0 {
		volumesEnv = strings.Join(volumes, ",")
	}
	childCmd.Env = append(os.Environ(), fmt.Sprintf("MYDOCKER_VOLUMES=%s", volumesEnv))
	// Setup namespaces
	childCmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET,
	}
	childCmd.Stdin = os.Stdin
	childCmd.Stdout = os.Stdout
	childCmd.Stderr = os.Stderr

	// Start container process
	if err := childCmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start container process: %v", err)
	}
	pid := childCmd.Process.Pid
	fmt.Printf("spawned child with PID %d\n", pid)

	// go func() {
	// 	err := childCmd.Wait()
	// 	if err != nil {
	// 		log.Printf("Container process %d exited with error: %v", pid, err)
	// 	} else {
	// 		log.Printf("Container process %d exited cleanly", pid)
	// 	}
	// 	log.Printf("Container Checkpoint")
	// }()

	// Setup network: create bridge if not exists
	exec.Command("ip", "link", "add", "mydocker0", "type", "bridge").Run()
	exec.Command("ip", "link", "set", "mydocker0", "up").Run()

	// Create veth pair for container networking
	prefix := id
	if len(id) > 5 {
		prefix = id[:5]
	}
	vethHost := "veth" + prefix
	vethCont := "eth0"
	exec.Command("ip", "link", "add", vethHost, "type", "veth", "peer", "name", vethCont).Run()
	// Attach container end of veth to container net namespace
	exec.Command("ip", "link", "set", vethCont, "netns", strconv.Itoa(pid)).Run()
	// Add host end of veth to bridge and bring up
	exec.Command("ip", "link", "set", vethHost, "master", "mydocker0").Run()
	exec.Command("ip", "link", "set", vethHost, "up").Run()
	// Assign IP inside container
	containerIP := "10.0.0.2"
	exec.Command("ip", "netns", "exec", strconv.Itoa(pid), "ip", "addr", "add", containerIP+"/24", "dev", vethCont).Run()
	exec.Command("ip", "netns", "exec", strconv.Itoa(pid), "ip", "link", "set", vethCont, "up").Run()

	// Setup port mappings via iptables
	for _, pm := range ports {
		exec.Command("iptables", "-t", "nat", "-A", "PREROUTING", "-p", "tcp",
			"--dport", strconv.Itoa(pm.HostPort),
			"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", containerIP, pm.ContainerPort)).Run()
		exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-j", "MASQUERADE").Run()
	}

	// Write container metadata to config.json
	info := ContainerInfo{
		ID:      id,
		Image:   image,
		Cmd:     cmd,
		Volumes: volumes,
		Ports:   ports,
		IP:      containerIP,
		PID:     pid,
	}
	configPath := filepath.Join(containerPath, "config.json")
	f, err := os.Create(configPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create config file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "    ")
	if err := enc.Encode(info); err != nil {
		return 0, fmt.Errorf("failed to write config: %v", err)
	}

	if err := childCmd.Wait(); err != nil {
		fmt.Printf("Error Waiting for command: %v\n", err)
		os.Exit(1)
	}

	return pid, nil
}

/* ───────────────────────────  Child  ──────────────────────────── */
// child runs as the container process inside the new namespaces.
func child(containerID string) error {
	basePath := "/var/lib/mydocker"
	containerPath := filepath.Join(basePath, "containers", containerID)
	bundlePath := filepath.Join(containerPath, "bundle")

	// Get the command to run from config.json or environment variable
	// cmdPath := filepath.Join(bundlePath, "rootfs", "bin", "sh") // default fallback
	// cmdPath := "/bin/sh"

	// Optionally load entrypoint from env or predefined args
	// cmdArgs := []string{cmdPath}

	// Mount volume if any
	volEnv := os.Getenv("MYDOCKER_VOLUMES")
	if volEnv != "" {
		volumes := strings.Split(volEnv, ",")
		for _, vol := range volumes {
			parts := strings.Split(vol, ":")
			if len(parts) != 2 {
				log.Printf("invalid volume format: %s", vol)
				continue
			}
			src := parts[0]
			dst := filepath.Join(bundlePath, "rootfs", parts[1])
			if err := os.MkdirAll(dst, 0755); err != nil {
				log.Printf("failed to create mount point %s: %v", dst, err)
				continue
			}
			if err := syscall.Mount(src, dst, "", syscall.MS_BIND, ""); err != nil {
				log.Printf("failed to bind mount %s to %s: %v", src, dst, err)
			}
		}
	}

	// Set hostname
	if err := syscall.Sethostname([]byte(containerID[:5])); err != nil {
		return fmt.Errorf("failed to set hostname: %v", err)
	}
	// Change root
	if err := syscall.Chroot(filepath.Join(bundlePath, "rootfs")); err != nil {
		return fmt.Errorf("failed to chroot: %v", err)
	}
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("failed to chdir: %v", err)
	}

	// Mount proc
	if err := syscall.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		return fmt.Errorf("failed to mount /proc: %v", err)
	}

	// Execute the specified command
	cmd := exec.Command(os.Args[3], os.Args[4:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	return nil
}

/* ───────────────────────────  Exec  ──────────────────────────── */

// func execContainer(containerID string, cmd []string) error {
// 	// Read container metadata
// 	configPath := filepath.Join("/var/lib/mydocker/containers", containerID, "config.json")
// 	data, err := os.ReadFile(configPath)
// 	if err != nil {
// 		return fmt.Errorf("failed to read config for container %s: %v", containerID, err)
// 	}

// 	var info ContainerInfo
// 	if err := json.Unmarshal(data, &info); err != nil {
// 		return fmt.Errorf("failed to parse config for container %s: %v", containerID, err)
// 	}

// 	// Lock this OS thread before setns calls
// 	runtime.LockOSThread()
// 	defer runtime.UnlockOSThread()

// 	// Join namespaces
// 	for _, ns := range []string{"pid", "uts", "mnt", "net"} {
// 		nsPath := fmt.Sprintf("/proc/%d/ns/%s", info.PID, ns)
// 		if _, err := os.Stat(fmt.Sprintf("/proc/%d", info.PID)); os.IsNotExist(err) {
// 			return fmt.Errorf("cannot exec: container PID %d not found, likely exited", info.PID)
// 		}

// 		f, err := os.Open(nsPath)
// 		if err != nil {
// 			return fmt.Errorf("opening namespace %s: %v", ns, err)
// 		}
// 		// if err := syscall.Setns(int(f.Fd()), 0); err != nil {
// 		// 	f.Close()
// 		// 	return fmt.Errorf("setns %s: %v", ns, err)
// 		// }
// 		// f.Close()
// 		defer f.Close()
// 		const SYS_SETNS = 308
// 		if _, _, err := syscall.Syscall(SYS_SETNS, f.Fd(), 0, 0); err != 0 {
// 			return fmt.Errorf("setns %s: %v", ns, err)
// 		}
// 	}

// 	// Chroot into the container's root filesystem
// 	rootfs := filepath.Join("/var/lib/mydocker/containers", containerID, "rootfs")
// 	if err := syscall.Chroot(rootfs); err != nil {
// 		return fmt.Errorf("chroot failed: %v", err)
// 	}
// 	if err := syscall.Chdir("/"); err != nil {
// 		return fmt.Errorf("chdir failed: %v", err)
// 	}

// 	// Execute the command inside the container
// 	execCmd := exec.Command(cmd[0], cmd[1:]...)
// 	execCmd.Stdin, execCmd.Stdout, execCmd.Stderr = os.Stdin, os.Stdout, os.Stderr
// 	if err := execCmd.Run(); err != nil {
// 		return fmt.Errorf("error running command: %v", err)
// 	}

// 	return nil
// }

func execContainer(containerID string, cmd []string) error {
	// Read container metadata
	configPath := filepath.Join("/var/lib/mydocker/containers", containerID, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config for container %s: %v", containerID, err)
	}

	var info ContainerInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("failed to parse config for container %s: %v", containerID, err)
	}

	rootfs := filepath.Join("/var/lib/mydocker/containers", containerID, "bundle/rootfs")

	args := append([]string{
		strconv.Itoa(info.PID),
		rootfs,
	}, cmd...)

	execCmd := exec.Command("./helper", args...)
	execCmd.Stdin, execCmd.Stdout, execCmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := execCmd.Run(); err != nil {
		return fmt.Errorf("error running command: %v", err)
	}

	return nil
}

/* ───────────────────────────  STOP Container  ────────────────────────── */
func stopContainer(id string) error {
	containerPath := filepath.Join("/var/lib/mydocker/containers", id)
	configPath := filepath.Join(containerPath, "config.json")

	// Read container config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config for container %s: %v", id, err)
	}

	var info ContainerInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("failed to parse config for container %s: %v", id, err)
	}

	// Kill the process
	if info.PID > 0 {
		if err := syscall.Kill(info.PID, syscall.SIGKILL); err != nil {
			return fmt.Errorf("failed to kill process %d: %v", info.PID, err)
		}
	}

	// Remove cgroup
	cgroups.RemoveCgroup(id)

	// Remove network setup
	vethHost := "veth" + id[:5]
	vethContainer := "eth0"
	if err := network.Cleanup(vethHost, vethContainer); err != nil {
		return fmt.Errorf("failed to clean up network: %v", err)
	}

	// Remove container directory
	if err := os.RemoveAll(containerPath); err != nil {
		return fmt.Errorf("failed to remove container directory %s: %v", containerPath, err)
	}

	fmt.Printf("Container %s stopped and cleaned up\n", id)
	return nil
}

/* ───────────────────────────  PULL Image  ────────────────────────── */

func pullImage(image string) error {
	// Base directory for images (adjust as needed)
	baseDir := "/var/lib/mydocker/images"

	// Parse image reference into repository and tag
	ref := image
	tag := "latest"
	if parts := strings.SplitN(ref, "@", 2); len(parts) > 1 {
		// Remove any digest suffix
		ref = parts[0]
	}
	// If the last colon is after the last slash, split tag
	if i := strings.LastIndex(ref, ":"); i != -1 && i > strings.LastIndex(ref, "/") {
		tag = ref[i+1:]
		ref = ref[:i]
	}

	// Build the directory path (preserving any registry in ref as part of path)
	dir := filepath.Join(baseDir, filepath.FromSlash(ref))

	// Create the directory (and any needed parents)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}

	// Use skopeo to copy the image into OCI layout format
	src := "docker://" + ref + ":" + tag
	dest := fmt.Sprintf("oci:%s:%s", dir, tag)
	cmd := exec.Command("skopeo", "copy", src, dest)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("skopeo copy failed: %s", string(output))
	}

	// Verify the layout: index.json and oci-layout must exist
	if _, err := os.Stat(filepath.Join(dir, "index.json")); os.IsNotExist(err) {
		return fmt.Errorf("invalid OCI layout: index.json not found in %s", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "oci-layout")); os.IsNotExist(err) {
		return fmt.Errorf("invalid OCI layout: oci-layout not found in %s", dir)
	}
	// Check that blobs/sha256 exists
	if stat, err := os.Stat(filepath.Join(dir, "blobs", "sha256")); err != nil || !stat.IsDir() {
		return fmt.Errorf("invalid OCI layout: blobs/sha256 not found or not a directory in %s", dir)
	}

	// Successfully pulled and validated
	return nil
}

/* ───────────────────────────  PS (List Containers)  ────────────────────────── */

func listContainers() ([]ContainerInfo, error) {
	containersDir := "/var/lib/mydocker/containers"
	files, err := os.ReadDir(containersDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read containers directory: %v", err)
	}

	fmt.Printf("CONTAINER ID\tPID\tIMAGE\tSTATUS\n")
	var containers []ContainerInfo
	for _, file := range files {
		if !file.IsDir() {
			continue // Skip non-directory files
		}
		id := file.Name()
		configPath := filepath.Join(containersDir, id, "config.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Printf("Error reading config for %s: %v\n", id, err)
			continue
		}

		var info ContainerInfo
		if err := json.Unmarshal(data, &info); err != nil {
			fmt.Printf("Error parsing config for %s: %v\n", id, err)
			continue
		}

		info.PID = -1 // Default PID if not found
		if procFile, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(info.PID), "status")); err == nil {
			lines := strings.Split(string(procFile), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "Pid:") {
					fields := strings.Fields(line)
					if len(fields) > 1 {
						info.PID, _ = strconv.Atoi(fields[1])
					}
				}
			}
		}

		fmt.Printf("%s\t%d\t%s\t%s\n", info.ID[:12], info.PID, info.Image, "running") // Simplified status
		containers = append(containers, info)
	}
	return containers, nil
}
