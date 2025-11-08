package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run test.go <num_keys>")
	}

	numKeys, err := strconv.Atoi(os.Args[1])
	if err != nil {
		log.Fatal("Invalid number of keys:", err)
	}

	// Initialize client with ClusterKit nodes
	nodes := []string{"localhost:8080", "localhost:8081", "localhost:8082"}
	client, err := NewClient(nodes)
	if err != nil {
		log.Fatal("Failed to create client:", err)
	}

	// Wait for topology to be ready
	time.Sleep(2 * time.Second)

	fmt.Printf("\n📝 Writing %d keys with quorum...\n", numKeys)
	start := time.Now()
	writeSuccess := 0
	writeFail := 0

	for i := 1; i <= numKeys; i++ {
		key := fmt.Sprintf("user:%d", i)
		value := fmt.Sprintf("User %d", i)

		if err := client.Set(key, value); err != nil {
			writeFail++
			if writeFail <= 5 {
				fmt.Printf("  ⚠️  Failed to write key %s: %v\n", key, err)
			}
		} else {
			writeSuccess++
		}

		if i%100 == 0 {
			fmt.Printf("  ✓ Wrote %d keys...\n", i)
		}
	}

	writeDuration := time.Since(start)
	writeOpsPerSec := float64(writeSuccess) / writeDuration.Seconds()

	fmt.Printf("\n✅ Write Results:\n")
	fmt.Printf("  Success: %d\n", writeSuccess)
	fmt.Printf("  Failed: %d\n", writeFail)
	fmt.Printf("  Duration: %v\n", writeDuration.Round(time.Millisecond))
	fmt.Printf("  Throughput: %.0f ops/sec\n", writeOpsPerSec)

	fmt.Printf("\n📖 Reading %d keys...\n", numKeys)
	start = time.Now()
	readSuccess := 0
	readFail := 0

	for i := 1; i <= numKeys; i++ {
		key := fmt.Sprintf("user:%d", i)

		if _, err := client.Get(key); err != nil {
			readFail++
			if readFail <= 5 {
				fmt.Printf("  ⚠️  Failed to read key %s: %v\n", key, err)
			}
		} else {
			readSuccess++
		}

		if i%100 == 0 {
			fmt.Printf("  ✓ Read %d keys...\n", i)
		}
	}

	readDuration := time.Since(start)
	readOpsPerSec := float64(readSuccess) / readDuration.Seconds()

	fmt.Printf("\n✅ Read Results:\n")
	fmt.Printf("  Success: %d\n", readSuccess)
	fmt.Printf("  Failed: %d\n", readFail)
	fmt.Printf("  Duration: %v\n", readDuration.Round(time.Millisecond))
	fmt.Printf("  Throughput: %.0f ops/sec\n", readOpsPerSec)
}
