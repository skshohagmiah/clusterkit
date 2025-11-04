package main

func (c *Cluster) WriteWAL(entry WALEntry) error {
	return nil
}
func (c *Cluster) ReadWAL() ([]WALEntry, error) {
	return nil, nil
}
func (c *Cluster) ClearWAL() error {
	return nil
}
