package main

import (
	"fmt"
)

func main() {
	fmt.Println("Hello, World!")

	// Start the HTTP server
	if err := StartHTTPServer(":8080"); err != nil {
		fmt.Printf("Error starting HTTP server: %v\n", err)
	}
}
