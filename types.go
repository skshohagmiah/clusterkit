package main

type Node struct {
	ID   string
	IP   string
	Name string
}

type Partition struct {
	ID       string
	Nodes    []string
	Replicas int
}

type PartitionMap struct {
	Partitions map[string]Partition
}

type Cluster struct {
	ID    string
	Name  string
	Nodes []Node
}

type Config struct {
	ClusterName string
	NodeCount   int
	Partitions  int
	Replicas    int
}

type Result struct {
	Success bool
	Message string
	Data    interface{}
}

type LogEntry struct {
	Timestamp string
	Level     string
	Created   string
}

type WALEntry struct {
	Operation string
	Data      interface{}
}
