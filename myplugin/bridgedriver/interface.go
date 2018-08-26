// interface
package bridgedriver

import (
	"net"

	"github.com/vishvananda/netlink"
)

type myBridgeInterface struct {
	bridge      *netlink.Bridge
	nlh         *netlink.Handle
	bridgeIPv4  *net.IPNet
	gatewayIPv4 net.IP
}

func newInterface(nlh *netlink.Handle, config *myBridgeConfiguration) (*myBridgeInterface, error) {
	i := &myBridgeInterface{nlh: nlh}

	if config.bridgeName == "" {
		config.bridgeName = DefaultBridgeName
	}

	return i, nil
}
