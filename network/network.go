package network

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const bridgeName = "br0"

/*
/// Example code to set up a bridge network interface

func SetupBridge() error {
    // Check if bridge exists
    br, err := netlink.LinkByName(bridgeName)
    if err == nil {
        return nil // Bridge already exists
    }

    // Create bridge
    br = &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName}}
    if err := netlink.LinkAdd(br); err != nil {
        return fmt.Errorf("failed to create bridge: %v", err)
    }

    // Assign IP to bridge
    addr, _ := netlink.ParseAddr("10.0.0.1/24")
    if err := netlink.AddrAdd(br, addr); err != nil {
        return fmt.Errorf("failed to assign IP to bridge: %v", err)
    }

    // Bring up the bridge
    if err := netlink.LinkSetUp(br); err != nil {
        return fmt.Errorf("failed to bring up bridge: %v", err)
    }

    return nil
}

*/

// Setup sets up network namespace and veth pair
func SetupNetwork(containerPid int, vethHost, vethContainer string) error {

	la := netlink.NewLinkAttrs()
	la.Name = vethHost
	veth := &netlink.Veth{
		LinkAttrs: la,
		PeerName:  vethContainer,
	}

	if err := netlink.LinkAdd(veth); err != nil {
		return fmt.Errorf("could not add veth pair: %v", err)
	}

	// Attach host end to bridge
	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("failed to get bridge: %v", err)
	}

	// Get link for vethHost
	hostLink, err := netlink.LinkByName(vethHost)
	if err != nil {
		return fmt.Errorf("could not get host veth link: %v", err)
	}

	if err := netlink.LinkSetMaster(hostLink, br); err != nil {
		return fmt.Errorf("could not set master for host veth: %v", err)
	}

	// bring up host veth
	if err := netlink.LinkSetUp(hostLink); err != nil {
		return fmt.Errorf("could not bring up host veth: %v", err)
	}

	// Bring up host end of veth
	if err := netlink.LinkSetUp(hostLink); err != nil {
		return fmt.Errorf("could not bring up host veth: %v", err)
	}

	// Get the network namespace of the container
	nsHandle, err := netns.GetFromPid(containerPid)
	if err != nil {
		return fmt.Errorf("could not get netns for pid %d: %v", containerPid, err)
	}
	defer nsHandle.Close()

	// Get link for vethContainer
	containerLink, err := netlink.LinkByName(vethContainer)
	if err != nil {
		return fmt.Errorf("could not get container veth link: %v", err)
	}

	// Move container end of veth to container's netns
	if err := netlink.LinkSetNsFd(containerLink, int(nsHandle)); err != nil {
		return fmt.Errorf("could not set netns for container veth: %v", err)
	}

	// Configure container's network interface inside its namespace
	err = configureContainerInterface(containerPid, vethContainer)
	if err != nil {
		return fmt.Errorf("could not configure container interface: %v", err)
	}

	return nil
}

// configureContainerInterface configures the container's network interface
func configureContainerInterface(pid int, ifName string) error {
	// Open container's network namespace
	nsHandle, err := netns.GetFromPid(pid)
	if err != nil {
		return fmt.Errorf("could not get netns for pid %d: %v", pid, err)
	}
	defer nsHandle.Close()

	// Save current network namespace
	origNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("could not get current netns: %v", err)
	}
	defer origNS.Close()

	// Set to container's network namespace
	if err := netns.Set(nsHandle); err != nil {
		return fmt.Errorf("could not set netns: %v", err)
	}
	defer netns.Set(origNS)

	// Now in container's netns
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("could not get link %s: %v", ifName, err)
	}

	// Assign IP address
	addr, err := netlink.ParseAddr("10.0.0.2/24")
	if err != nil {
		return fmt.Errorf("could not parse IP address: %v", err)
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("could not add IP address: %v", err)
	}

	// Bring up interface
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("could not bring up interface: %v", err)
	}

	// Add default route
	gw := net.ParseIP("10.0.0.1")
	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Gw:        gw,
	}

	if err := netlink.RouteAdd(route); err != nil {
		return fmt.Errorf("could not add route: %v", err)
	}

	return nil
}

func Cleanup(vethHost, vethContainer string) error {
	// Delete host-side veth interface
	link, err := netlink.LinkByName(vethHost)
	if err == nil {
		if err := netlink.LinkDel(link); err != nil {
			return fmt.Errorf("failed to delete host veth %s: %v", vethHost, err)
		}
	}

	// Note: Deleting one end of the veth pair removes both ends

	return nil
}
