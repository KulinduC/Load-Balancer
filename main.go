package main

import (
	"fmt"
	"loadbalancer/loadbalancer"
	"loadbalancer/servers"
	"sync"
	"time"
)

func main() {
	// Number of backend servers
	amount := 5

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
		fmt.Println("Starting load balancer on port 8090...")
		loadbalancer.MakeLoadBalancer(amount)
	}()

	// Wait for both to complete (they run indefinitely)
	wg.Wait()
}