package bridgedriver

import (
	"fmt"

	"github.com/docker-demo/myplugin/utils/netutils"
	"github.com/vishvananda/netlink"
)

type setupStep func(*myBridgeConfiguration, *myBridgeInterface) error

type bridgeSetup struct {
	config *myBridgeConfiguration
	bridge *myBridgeInterface
	steps  []setupStep
}

func newBridgeSetup(c *myBridgeConfiguration, i *myBridgeInterface) *bridgeSetup {
	return &bridgeSetup{config: c, bridge: i}
}

func (b *bridgeSetup) apply() error {
	for _, fn := range b.steps {
		if err := fn(b.config, b.bridge); err != nil {
			return err
		}
	}

	return nil
}

func (b *bridgeSetup) queueStep(step setupStep) {
	b.steps = append(b.steps, step)
}

func setupBridgeDevice(config *myBridgeConfiguration, i *myBridgeInterface) error {
	var err error

	i.bridge = &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: config.bridgeName,
		},
	}

	if err = i.nlh.LinkAdd(i.bridge); err != nil {
		return fmt.Errorf("[setup] failed to create bridge %s", config.bridgeName)
	}

	hwAddr := netutils.GenerateRandomMAC()
	if err = i.nlh.LinkSetHardwareAddr(i.bridge, hwAddr); err != nil {
		return fmt.Errorf("[setup] failed to set bridge mac-address")
	}

	return nil
}

func removeBridgeDevice(config *myBridgeConfiguration, i *myBridgeInterface) error {
	var err error

	if _, err = i.nlh.LinkByName(config.bridgeName); err != nil {
		return fmt.Errorf("[remove] failed to retrive link for interface %s", config.bridgeName)
	}

	if err = i.nlh.LinkDel(i.bridge); err != nil {
		return fmt.Errorf("[remove] failed to delete bridge for interface %s", config.bridgeName)
	}

	fmt.Printf("[remove] bridge %s", config.bridgeName)
	return nil
}

func setupBridgeIPv4(config *myBridgeConfiguration, i *myBridgeInterface) error {
	if err := i.nlh.AddrAdd(i.bridge, &netlink.Addr{IPNet: config.addressIPv4}); err != nil {
		return fmt.Errorf("[setup]failed to add addr to bridge")
	}

	i.bridgeIPv4 = config.addressIPv4
	i.gatewayIPv4 = config.addressIPv4.IP

	return nil
}

func setBridgeUp(config *myBridgeConfiguration, i *myBridgeInterface) error {
	if err := i.nlh.LinkSetUp(i.bridge); err != nil {
		return fmt.Errorf("[setup] failed to set link up for %s", config.bridgeName)
	}

	return nil
}

func setBridgeDown(i *myBridgeInterface) error {
	if err := i.nlh.LinkSetDown(i.bridge); err != nil {
		return fmt.Errorf("[setup] failed to set up")
	}

	return nil
}
