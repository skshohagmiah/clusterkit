package clusterkit

import (
	"errors"
	"fmt"
)

func (c *Cluster) AddNode(node Node) error {
	if node.ID == "" || node.IP == "" || node.Name == "" {
		return errors.New("invalid node data")
	}
	c.Nodes = append(c.Nodes, node)
	c.rebuildNodeMap()
	return nil
}

// GetNodeByID returns a node by ID - O(1) using NodeMap
func (c *Cluster) GetNodeByID(nodeID string) (*Node, error) {
	if node, exists := c.NodeMap[nodeID]; exists {
		return node, nil
	}
	return nil, fmt.Errorf("node %s not found", nodeID)
}

// rebuildNodeMap rebuilds the NodeMap from the Nodes slice for O(1) lookups
func (c *Cluster) rebuildNodeMap() {
	if c.NodeMap == nil {
		c.NodeMap = make(map[string]*Node, len(c.Nodes))
	} else {
		// Clear existing map
		for k := range c.NodeMap {
			delete(c.NodeMap, k)
		}
	}
	
	for i := range c.Nodes {
		c.NodeMap[c.Nodes[i].ID] = &c.Nodes[i]
	}
	
	fmt.Printf("[DEBUG] rebuildNodeMap: %d nodes -> %d NodeMap entries\n", len(c.Nodes), len(c.NodeMap))
}

func (c *Cluster) ListNodes() []Node {
	return c.Nodes
}
func (c *Cluster) RemoveNode(nodeID string) error {
	for i, node := range c.Nodes {
		if node.ID == nodeID {
			c.Nodes = append(c.Nodes[:i], c.Nodes[i+1:]...)
			c.rebuildNodeMap()
			return nil
		}
	}
	return errors.New("node not found")
}
