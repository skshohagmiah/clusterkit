package main

import (
	"errors"
	"fmt"
	http "net/http"
)

func StartHTTPServer(addr string) error {
	if addr == "" {
		return errors.New("address cannot be empty")
	}

	server := &http.Server{Addr: addr}

	server.SetKeepAlivesEnabled(true)
	server.Handler = http.DefaultServeMux

	http.HandleFunc("/listen", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Listening on %s\n", addr)

	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	return nil
}

func SendLogMessage(cluster Cluster) error {
	if len(cluster.Nodes) == 0 {
		return errors.New("cluster cannot be empty")
	}

	for _, node := range cluster.Nodes {
		fmt.Printf("Sending log to node %s at %s\n", node.Name, node.IP)
		httpResp, err := http.Post(fmt.Sprintf("http://%s/listen", node.IP), "application/json", nil)
		if err != nil {
			return fmt.Errorf("failed to send log to node %s: %v", node.Name, err)
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return fmt.Errorf("node %s responded with status %d", node.Name, httpResp.StatusCode)
		}
	}
	return nil
}
