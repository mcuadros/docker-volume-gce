package providers

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"

	"google.golang.org/api/compute/v1"
)

var (
	NetworkBaseName    = "docker-container-network-%s-%s"
	DeviceNameBaseName = "docker-volume-%s"
)

type DiskConfig struct {
	Name           string
	Type           string
	SizeGb         int64
	SourceSnapshot string
	SourceImage    string
}

func (c *DiskConfig) Disk() *compute.Disk {
	return &compute.Disk{
		Name:           c.Name,
		Type:           c.Type,
		SizeGb:         c.SizeGb,
		SourceSnapshot: c.SourceSnapshot,
		SourceImage:    c.SourceImage,
	}
}

func (c *DiskConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("invalid disk config, name field cannot be empty")
	}

	if c.SourceSnapshot != "" && c.SourceImage != "" {
		return fmt.Errorf("invalid dick config, source snapshot and source image can't be presents at the same time.")
	}

	return nil
}

func (c *DiskConfig) DeviceName() string {
	return fmt.Sprintf(DeviceNameBaseName, c.Name)
}

type SessionAffinity string
type NetworkConfig struct {
	GroupName string
	Container string
	Protocol  string
	Network   string
	Address   string
	Port      string
	Source    struct {
		Ranges []string
		Tags   []string
	}
	SessionAffinity SessionAffinity
}

func (c *NetworkConfig) TargetPool(project, zone, instance string) *compute.TargetPool {
	return &compute.TargetPool{
		Name:            c.Name(instance),
		Instances:       []string{InstanceURL(project, zone, instance)},
		SessionAffinity: string(c.SessionAffinity),
	}
}

func (c *NetworkConfig) ForwardingRule(instance, targetPoolURL string) *compute.ForwardingRule {
	return &compute.ForwardingRule{
		Name:       c.Name(instance),
		IPAddress:  c.Address,
		IPProtocol: c.Protocol,
		PortRange:  c.Port,
		Target:     targetPoolURL,
	}
}

func (c *NetworkConfig) Firewall(instance string) *compute.Firewall {
	sourceRanges := c.Source.Ranges
	if len(c.Source.Ranges) == 0 && len(c.Source.Tags) == 0 {
		sourceRanges = []string{"0.0.0.0/0"}
	}

	network := c.Network
	if len(network) == 0 {
		network = "global/networks/default"
	}

	name := c.Name(instance)

	return &compute.Firewall{
		Name:         name,
		SourceRanges: sourceRanges,
		SourceTags:   c.Source.Tags,
		TargetTags:   []string{name},
		Network:      network,
		Allowed: []*compute.FirewallAllowed{{
			IPProtocol: c.Protocol,
			Ports:      []string{c.Port},
		}},
	}
}

func (c *NetworkConfig) Name(instance string) string {
	return fmt.Sprintf(NetworkBaseName, c.Group(instance), c.ID(instance))
}

func (c *NetworkConfig) Group(instance string) string {
	if c.GroupName != "" {
		return c.GroupName
	}

	return fmt.Sprintf("%s-%s", c.Container, instance)
}

func (c *NetworkConfig) ID(instance string) string {
	unique := strings.Join([]string{
		c.Group(instance),
		c.Address, c.Protocol, c.Port,
	}, "|")

	hash := md5.Sum([]byte(unique))
	return hex.EncodeToString(hash[:])[:8]
}

func (c *NetworkConfig) Validate() error {
	if c.Container == "" {
		return fmt.Errorf("invalid network config, container field cannot be empty")
	}

	if c.Protocol == "" {
		return fmt.Errorf("invalid network config, protocol field cannot be empty")
	}

	if c.Port == "" {
		return fmt.Errorf("invalid network config, port field cannot be empty")
	}

	return nil
}