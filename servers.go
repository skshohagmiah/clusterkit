package clusterkit

import (
	"errors"
)

func (c *Cluster) AddNode(node Node) error {
	if node.ID == "" || node.IP == "" || node.Name == "" {
		return errors.New("invalid node data")
	}
	c.Nodes = append(c.Nodes, node)
	return nil
}

func (c *Cluster) GetNodeByID(nodeID string) (*Node, error) {
	for _, node := range c.Nodes {
		if node.ID == nodeID {
			return &node, nil
		}
	}
	return nil, errors.New("node not found")
}

func (c *Cluster) ListNodes() []Node {
	return c.Nodes
}

func (c *Cluster) RemoveNode(nodeID string) error {
	for i, node := range c.Nodes {
		if node.ID == nodeID {
			c.Nodes = append(c.Nodes[:i], c.Nodes[i+1:]...)
			return nil
		}
	}
	return errors.New("node not found")
}
