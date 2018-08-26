package bridgedriver

import (
	"fmt"
	"net"

	"github.com/docker-demo/myplugin/ns"
	"github.com/docker-demo/myplugin/utils/netutils"
	"github.com/docker/go-plugins-helpers/network"
	"github.com/vishvananda/netlink"
)

const (
	DefaultBridgeName = "docker0"
	vethPrefix        = "veth"
	vethLen           = 7
)

type myBridgeEndpoint struct {
	id         string
	nid        string
	srcName    string
	addr       *net.IPNet
	macAddress net.HardwareAddr
}

type myBridgeNetwork struct {
	id        string
	config    *myBridgeConfiguration
	bridge    *myBridgeInterface
	driver    *myDriver
	endpoints map[string]*myBridgeEndpoint
}

type myBridgeConfiguration struct {
	id                 string
	bridgeName         string
	mtu                int
	addressIPv4        *net.IPNet
	defaultGatewayIPv4 net.IP
}

type myDriver struct {
	networks map[string]*myBridgeNetwork
	nlh      *netlink.Handle
}

func NewNetworkDriver() network.Driver {
	driver := &myDriver{
		networks: map[string]*myBridgeNetwork{},
	}
	return driver
}

func (d *myDriver) GetCapabilities() (*network.CapabilitiesResponse, error) {
	resp := &network.CapabilitiesResponse{Scope: "local"}
	return resp, nil
}

func (d *myDriver) AllocateNetwork(request *network.AllocateNetworkRequest) (*network.AllocateNetworkResponse, error) {
	resp := &network.AllocateNetworkResponse{}
	return resp, nil
}

func (d *myDriver) FreeNetwork(request *network.FreeNetworkRequest) error {
	return nil
}

func (c *myBridgeConfiguration) processIPAM(id string, ipamV4Data []*network.IPAMData) error {
	var err error

	if len(ipamV4Data) > 1 {
		return fmt.Errorf("[processIPAM] myBridge doesn't support multiple subnets")
	}

	if len(ipamV4Data) == 0 {
		return fmt.Errorf("[processIPAM] %s requires ipv4 configuration", id)
	}

	if _, c.addressIPv4, err = net.ParseCIDR(ipamV4Data[0].Pool); err != nil {
		return fmt.Errorf("[processIPAM] IP Configuration error")
	}

	return nil
}

func (d *myDriver) getNetworks() []*myBridgeNetwork {
	list := make([]*myBridgeNetwork, 0, len(d.networks))

	for _, n := range d.networks {
		list = append(list, n)
	}

	return list
}

func (d *myDriver) createNetwork(config *myBridgeConfiguration) error {
	if d.nlh == nil {
		d.nlh = ns.NlHandle()
	}

	bridgeIface, err := newInterface(d.nlh, config)
	if err != nil {
		return err
	}

	network := &myBridgeNetwork{
		id:        config.id,
		config:    config,
		bridge:    bridgeIface,
		driver:    d,
		endpoints: make(map[string]*myBridgeEndpoint),
	}

	d.networks[config.id] = network
	defer func() {
		if err != nil {
			delete(d.networks, config.id)
		}
	}()

	bridgeSetup := newBridgeSetup(config, bridgeIface)

	bridgeSetup.queueStep(setupBridgeDevice)

	bridgeSetup.queueStep(setupBridgeIPv4)

	bridgeSetup.queueStep(setBridgeUp)

	return bridgeSetup.apply()
}

func (d *myDriver) CreateNetwork(request *network.CreateNetworkRequest) error {
	for _, ip := range request.IPv4Data {
		fmt.Printf("request IPAM Data Address Space %s Pool %s Gateway %s", ip.AddressSpace, ip.Pool, ip.Gateway)
	}

	nid := request.NetworkID

	if _, ok := d.networks[nid]; !ok {
		fmt.Errorf("network %s exists", nid)
	}

	config := &myBridgeConfiguration{
		bridgeName: "br-" + nid[:12],
		id:         nid,
	}

	if err := config.processIPAM(nid, request.IPv4Data); err != nil {
		return err
	}

	if err := d.createNetwork(config); err != nil {
		return err
	}

	return nil
}

func (d *myDriver) DeleteNetwork(request *network.DeleteNetworkRequest) error {

	var err error

	nid := request.NetworkID

	fmt.Printf("DeleteNetwork %s", request.NetworkID)

	n, ok := d.networks[nid]
	if !ok {
		return fmt.Errorf("[DeleteNetwork]network %s doesn't exist", nid)
	}

	for _, ep := range n.endpoints {
		if link, err := d.nlh.LinkByName(ep.srcName); err == nil {
			if err := d.nlh.LinkDel(link); err != nil {
				fmt.Printf("[DeleteNetwork] failed to delete interface %s", ep.srcName)
			}
		}
	}

	if err = setBridgeDown(n.bridge); err != nil {
		return fmt.Errorf("[DeleteNetwork]failed to set link down for %s", n.config.bridgeName)
	}

	if err = removeBridgeDevice(n.config, n.bridge); err != nil {
		return fmt.Errorf("[DeleteNetwork]failed to remove %s", n.config.bridgeName)
	}

	delete(d.networks, nid)

	return nil
}

func (d *myDriver) CreateEndpoint(request *network.CreateEndpointRequest) (*network.CreateEndpointResponse, error) {
	var err error

	fmt.Printf("[CreateEndpoint] nid %s, eid %s", request.NetworkID, request.EndpointID)
	response := &network.CreateEndpointResponse{}

	if request.Interface == nil {
		return response, fmt.Errorf("[CreateEndpoint] interface invalid")
	}

	nid := request.NetworkID
	eid := request.EndpointID

	n, ok := d.networks[nid]
	if !ok {
		return response, fmt.Errorf("[CreateEndpoint] network %s not found", nid)
	}

	// Create myBridgeEndpoint
	endpoint := &myBridgeEndpoint{id: eid, nid: nid}
	n.endpoints[eid] = endpoint
	defer func() {
		if err != nil {
			delete(n.endpoints, eid)
		}
	}()

	hostIfName, err := netutils.GenerateIfaceName(d.nlh, vethPrefix, vethLen)
	if err != nil {
		return response, fmt.Errorf("[CreateEndpoint] failed to generate hostIfName %s", eid)
	}

	containerIfName, err := netutils.GenerateIfaceName(d.nlh, vethPrefix, vethLen)
	if err != nil {
		return response, fmt.Errorf("[CreateEndpoint] failed to generate containerIfName %s", eid)
	}

	endpoint.srcName = containerIfName

	/* Create Veth device
	   type Veth struct {
		   LinkAttrs
		   PeerName string

	   }
	*/
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{Name: hostIfName, TxQLen: 0},
		PeerName:  containerIfName,
	}
	err = d.nlh.LinkAdd(veth)
	if err != nil {
		return response, fmt.Errorf("[CreateEndpoint] failed to add the veth device")
	}

	hostside, err := d.nlh.LinkByName(hostIfName)
	if err != nil {
		return response, fmt.Errorf("[CreateEndpoint] failed to get host side interface")
	}
	defer func() {
		if err != nil {
			d.nlh.LinkDel(hostside)
		}
	}()

	containerside, err := d.nlh.LinkByName(containerIfName)
	if err != nil {
		return response, fmt.Errorf("[CreateEndpoint] failed to get container side interface")
	}
	defer func() {
		if err != nil {
			d.nlh.LinkDel(containerside)
		}
	}()

	config := n.config
	if config.mtu != 0 {
		err = d.nlh.LinkSetMTU(hostside, config.mtu)
		if err != nil {
			return response, fmt.Errorf("[CreateEndpoint] failed to set MTU on hostside")
		}
		err = d.nlh.LinkSetMTU(containerside, config.mtu)
		if err != nil {
			return response, fmt.Errorf("[CreateEndpoint] failed to set MTU on containerside")
		}
	}

	err = d.nlh.LinkSetMaster(hostside, n.bridge.bridge)
	if err != nil {
		return response, fmt.Errorf("[CreateEndpoint] failed to add hostside to Bridge")
	}

	return response, nil
}

func (d *myDriver) DeleteEndpoint(request *network.DeleteEndpointRequest) error {

	nid := request.NetworkID
	eid := request.EndpointID

	n, ok := d.networks[nid]
	if !ok {
		return fmt.Errorf("[DeleteEndpoint] network %s not found", nid)
	}

	ep, ok := n.endpoints[eid]
	if !ok {
		return fmt.Errorf("[DeleteEndpoint] Endpoint %s not found", eid)
	}

	link, err := d.nlh.LinkByName(ep.srcName)
	if err != nil {
		return fmt.Errorf("[DeleteEndpoint] failed to get interface %s", ep.srcName)
	}

	err = d.nlh.LinkDel(link)
	if err != nil {
		return fmt.Errorf("[DeleteEndpoint] failed to delete interface %s", ep.srcName)
	}

	delete(n.endpoints, eid)

	return nil
}

func (d *myDriver) EndpointInfo(request *network.InfoRequest) (*network.InfoResponse, error) {
	return nil, nil
}

func (d *myDriver) Join(request *network.JoinRequest) (*network.JoinResponse, error) {
	resp := &network.JoinResponse{}

	nid := request.NetworkID
	eid := request.EndpointID

	n, ok := d.networks[nid]
	if !ok {
		return resp, fmt.Errorf("[Join] network %s not found", nid)
	}

	ep, ok := n.endpoints[eid]
	if !ok {
		return resp, fmt.Errorf("[Join] endpoint %s not found", ep)
	}

	resp.InterfaceName.SrcName = ep.srcName
	resp.InterfaceName.DstPrefix = "eth"

	return resp, nil
}

func (d *myDriver) Leave(request *network.LeaveRequest) error {
	return nil
}

func (d *myDriver) DiscoverNew(request *network.DiscoveryNotification) error {
	return nil
}

func (d *myDriver) DiscoverDelete(request *network.DiscoveryNotification) error {
	return nil
}

func (d *myDriver) ProgramExternalConnectivity(*network.ProgramExternalConnectivityRequest) error {
	return nil
}

func (d *myDriver) RevokeExternalConnectivity(*network.RevokeExternalConnectivityRequest) error {
	return nil
}
