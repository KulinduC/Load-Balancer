package main

import (
	"fmt"
	"loadbalancer/loadbalancer"
	"loadbalancer/servers"
	"os"
	"sync"
	"time"
)

func main() {
	// Number of backend servers
	amount := 4

	// Algorithm to use - can be set via command line argument
	// Options: RoundRobin, LeastConnections, IPHash, WeightedRoundRobin
	algorithm := loadbalancer.RoundRobin

	// Check for command line argument to override algorithm
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "RoundRobin":
			algorithm = loadbalancer.RoundRobin
		case "LeastConnections":
			algorithm = loadbalancer.LeastConnections
		case "IPHash":
			algorithm = loadbalancer.IPHash
		case "WeightedRoundRobin":
			algorithm = loadbalancer.WeightedRoundRobin
		default:
			fmt.Printf("Unknown algorithm: %s. Using default: %s\n", os.Args[1], algorithm)
		}
	}

	// WaitGroup to coordinate goroutines
	var wg sync.WaitGroup
	wg.Add(2) // One for servers, one for load balancer

	// Start backend servers in a goroutine
	go func() {
		defer wg.Done()
		fmt.Println("Starting backend servers...")
		servers.RunServers(amount)
	}()

	// Give servers time to start up
	time.Sleep(2 * time.Second)

	// Start load balancer in a goroutine
	go func() {
		defer wg.Done()
		fmt.Printf("Starting load balancer on port 8090 with %s algorithm...\n", algorithm)
		loadbalancer.MakeLoadBalancer(amount, algorithm)
	}()

	// Wait for both to complete (they run indefinitely)
	wg.Wait()
}