package main

// This is an example file showing how to use the client SDK
// To run: go run example_usage.go

import (
	"fmt"
	"log"
	
	"github.com/skshohagmiah/clusterkit/client"
)

// NOTE: This file is in package main for demonstration purposes
// In your application, you would import the client package

func main() {
	// Developer's application code
	
	// Step 1: Initialize the client with your cluster nodes
	kvClient := client.NewKVClient([]string{
		"kv-node1.yourcompany.com:9080",
		"kv-node2.yourcompany.com:9080",
		"kv-node3.yourcompany.com:9080",
		// The SDK will round-robin between nodes
	})
	
	// Step 2: Use it like any key-value store!
	
	// Store data
	err := kvClient.Set("user:123", `{"name": "John", "email": "john@example.com"}`)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("✓ Stored user:123")
	
	// Retrieve data
	value, err := kvClient.Get("user:123")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("✓ Retrieved: %s\n", value)
	
	// Store more data
	kvClient.Set("session:abc", `{"token": "xyz", "expires": 1234567890}`)
	kvClient.Set("cache:homepage", "<html>...</html>")
	
	// Batch operations for efficiency
	kvClient.SetBatch([]client.BatchOperation{
		{Key: "product:1", Value: `{"name": "Laptop", "price": 999}`},
		{Key: "product:2", Value: `{"name": "Mouse", "price": 29}`},
		{Key: "product:3", Value: `{"name": "Keyboard", "price": 79}`},
	})
	fmt.Println("✓ Batch stored 3 products")
	
	// Delete data
	err = kvClient.Delete("cache:homepage")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("✓ Deleted cache:homepage")
}

// Example: Using in a web application
func webAppExample() {
	kv := client.NewKVClient([]string{
		"kv1.example.com:9080",
		"kv2.example.com:9080",
		"kv3.example.com:9080",
	})
	
	// Store session data
	sessionID := "sess_abc123"
	sessionData := `{"user_id": 456, "logged_in": true}`
	kv.Set(sessionID, sessionData)
	
	// Retrieve session data
	data, _ := kv.Get(sessionID)
	fmt.Printf("Session: %s\n", data)
	
	// Cache expensive queries
	cacheKey := "query:top_products"
	kv.Set(cacheKey, `[{"id": 1, "name": "Laptop"}, {"id": 2, "name": "Phone"}]`)
	
	// Later...
	cachedResult, _ := kv.Get(cacheKey)
	fmt.Printf("Cached result: %s\n", cachedResult)
}
