package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// NodeConfig is a static configuration for a committee node in MVP.
type NodeConfig struct {
	NodeID           string            `json:"node_id"`
	ListenAddr       string            `json:"listen_addr"`
	StaticNodes      []string          `json:"static_nodes"`
	StaticNodeAddrs  map[string]string `json:"static_node_addrs"`
	ControlAddr      string            `json:"control_addr"`
	CommitteeSize    int               `json:"committee_size"`
	Threshold        int               `json:"threshold"`
	CoordinatorID    string            `json:"coordinator_id"`
	DomainSeparation string  `json:"domain_separation"`
	MessageVersion   string            `json:"message_version"`
}

func Load(path string) (*NodeConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	var cfg NodeConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return &cfg, nil
}

func (c *NodeConfig) Validate() error {
	if c == nil {
		return errors.New("nil config")
	}
	if c.NodeID == "" {
		return errors.New("node_id is required")
	}
	if c.ListenAddr == "" {
		return errors.New("listen_addr is required")
	}
	if c.CommitteeSize <= 0 {
		return errors.New("committee_size must be > 0")
	}
	if c.Threshold <= 0 || c.Threshold > c.CommitteeSize {
		return errors.New("threshold must be in range (0, committee_size]")
	}
	if len(c.StaticNodes) == 0 {
		return errors.New("static_nodes cannot be empty")
	}
	if len(c.StaticNodeAddrs) == 0 {
		return errors.New("static_node_addrs cannot be empty")
	}
	for _, nodeID := range c.StaticNodes {
		addr, ok := c.StaticNodeAddrs[nodeID]
		if !ok || addr == "" {
			return fmt.Errorf("missing static_node_addrs entry for %s", nodeID)
		}
	}
	if c.CoordinatorID == "" {
		return errors.New("coordinator_id is required")
	}
	if c.DomainSeparation == "" {
		return errors.New("domain_separation is required")
	}
	if c.MessageVersion == "" {
		return errors.New("message_version is required")
	}
	return nil
}
