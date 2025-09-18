package loadbalancer

import (
	"container/heap"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type onCloseRC struct {
  io.ReadCloser
  onClose func()
}
func (r *onCloseRC) Close() error {
  err := r.ReadCloser.Close()
  if r.onClose != nil { r.onClose() }
  return err
}

type Algorithm string

const (
	RoundRobin         Algorithm = "RoundRobin"
	LeastConnections   Algorithm = "LeastConnections"
	IPHash             Algorithm = "IPHash"
	WeightedRoundRobin Algorithm = "WeightedRoundRobin"
)

var (
	baseURL = "http://localhost:808"
)

type LoadBalancer struct {
	Algo     Algorithm
}

type EndPoints struct {
	List              []*url.URL
	currIndex         int   // tracks index for round robin
	activeConnections []int // tracks active connections for each server
	// For weighted round robin
	weights       []int // defines how many request each server can handle
	// For heap-based algorithms
	connHeap   *ServerHeap // min-heap for least connections
	weightHeap *ServerHeap // max-heap for weighted round robin
	algorithm  Algorithm   // tracks algorithm for heap
	mutex      sync.Mutex  // mutex for locking and unlocking the heap
	healthStatus []bool      // tracks health status of each server
	lastCheck    []time.Time // tracks last health check time
}

func (e *EndPoints) DebugServers() {
	fmt.Println("=== SERVER DEBUG INFO ===")
	fmt.Printf("Total servers: %d\n", len(e.List))
	fmt.Printf("Algorithm: %s\n", e.algorithm)
	if e.algorithm == LeastConnections {
		fmt.Printf("Heap length: %d\n", e.connHeap.Len())
	}
	if e.algorithm == WeightedRoundRobin {
		fmt.Printf("Heap length: %d\n", e.weightHeap.Len())
	}
	fmt.Println()


	for i := 0; i < len(e.List); i++ {
		healthy := e.isServerHealthy(i)
		url := e.List[i].String()

		fmt.Printf("Server %d:\n", i)
    fmt.Printf("  URL: %s\n", url)
    fmt.Printf("  Healthy: %t\n", healthy)
		if e.algorithm == LeastConnections {
    	fmt.Printf("  Server Connections: %d\n", e.activeConnections[i])
		}
		if e.algorithm == WeightedRoundRobin {
			fmt.Printf("  Server Weight: %d\n",e.weights[i])
		}
    fmt.Printf("  Last Health Check: %v\n", e.lastCheck[i])
    fmt.Println()
	}

	if e.algorithm == WeightedRoundRobin {
		if e.weightHeap != nil && e.weightHeap.Len() > 0 {
			fmt.Println("=== HEAP CONTENTS ===")
			for i := 0; i < e.weightHeap.Len(); i++ {
				node := e.weightHeap.nodes[i]
				fmt.Printf("Heap[%d]: Server %d, Weights %d\n",
                i, node.Index, node.Weight * -1)
			}
		}
	}

	if e.algorithm == LeastConnections {
		if e.connHeap != nil && e.connHeap.Len() > 0 {
			fmt.Println("=== HEAP CONTENTS ===")
			for i := 0; i < e.connHeap.Len(); i++ {
				node := e.connHeap.nodes[i]
				fmt.Printf("Heap[%d]: Server %d, Connections %d\n",
                i, node.Index, node.Connections)
			}
		}
	}
	fmt.Println("=========================")
}

// Initialize heaps based on algorithm
func (e *EndPoints) Initialize(algorithm Algorithm) {
	e.algorithm = algorithm

	switch algorithm {
	case LeastConnections:
		// Only initialize connection heap for least connections
		e.connHeap = ServerHeapConstructor(LeastConnectionsComparer{})
		heap.Init(e.connHeap)

		// Add all servers to connection heap
		for i := range e.List {
			heap.Push(e.connHeap, &ServerNode{
				Index:       i,
				Connections: 0,
				Weight:      0,
			})
		}
		e.weightHeap = nil

	case WeightedRoundRobin:
		// Only initialize weight heap for weighted round robin
		e.weightHeap = ServerHeapConstructor(WeightComparer{})
		heap.Init(e.weightHeap)

		// Add all servers to weight heap (use negative weights for max-heap)
		for i := range e.List {
			heap.Push(e.weightHeap, &ServerNode{
				Index:       i,
				Connections: 0,
				Weight:      -e.weights[i], // Negative for max-heap
			})
		}
		e.connHeap = nil

	default:
		// For RoundRobin and IPHash, no heap needed
		e.connHeap = nil
		e.weightHeap = nil
	}
}


// Better health check with caching and timeout
func (e *EndPoints) isServerHealthy(index int) bool {
	if index >= len(e.healthStatus) {
		return false
	}

	// If we checked recently (within 5 seconds), return cached result
	if time.Since(e.lastCheck[index]) < 5*time.Second {
		return e.healthStatus[index]
	}

	// Perform health check with timeout
	healthy := e.quickHealthCheck(index)
	e.healthStatus[index] = healthy
	e.lastCheck[index] = time.Now()
	return healthy
}

// Quick health check with timeout
func (e *EndPoints) quickHealthCheck(index int) bool {
	serverURL := e.List[index].String()

	// Create client with 2 second timeout
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	resp, err := client.Get(serverURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Consider healthy if status is 200-299
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}


func (e *EndPoints) decrementConnection(serverIndex int) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if serverIndex >= 0 && serverIndex < len(e.activeConnections) && e.activeConnections[serverIndex] > 0 && e.connHeap != nil {
		e.activeConnections[serverIndex]--
		log.Printf("Decremented connection count for server %d. New count: %d", serverIndex, e.activeConnections[serverIndex])
		log.Print(e.activeConnections)
		for i := 0; i < e.connHeap.Len(); i++ {
			if e.connHeap.nodes[i].Index == serverIndex {
				e.connHeap.nodes[i].Connections = e.activeConnections[serverIndex]
				heap.Fix(e.connHeap,i)
				break
			}
		}
	}
}

// Round Robin
func (e *EndPoints) GetServerRR() *url.URL {
	if len(e.List) == 0 {
		return nil
	}

	index := e.currIndex
	e.currIndex = (e.currIndex + 1) % len(e.List)
	return e.List[index]
}
// Least Connections
func (e *EndPoints) GetServerLC() (*url.URL, int) {
	if len(e.List) == 0 {
		return nil, -1
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.connHeap.Len() == 0 {
		e.Initialize(LeastConnections)
	}

	for e.connHeap.Len() > 0 {
		node := e.connHeap.Top()
		if e.isServerHealthy(node.Index) {
			e.activeConnections[node.Index]++
			node.Connections = e.activeConnections[node.Index]
			heap.Fix(e.connHeap, 0)
			e.DebugServers()
			return e.List[node.Index], node.Index
		} else {
			heap.Pop(e.connHeap)
		}
	}
	return nil, -1
}

// IP Hash
func (e *EndPoints) GetServerIPHash(clientIP string) *url.URL {
	if len(e.List) == 0 {
		return nil
	}

	ip := strings.Split(clientIP, ":")[0]
	// MD5 hash of the ip
	// 16 bytes
	hash := md5.Sum([]byte(ip))

	// Convert first 4 bytes to integer
	// each byte is 8 bits, so take 32 bits to create an integer
	hashInt := int(hash[0])<<24 + int(hash[1])<<16 + int(hash[2])<<8 + int(hash[3])
	serverIndex := hashInt % len(e.List)
	return e.List[serverIndex]
}

// Weighted Round Robin using heap
func (e *EndPoints) GetServerWRR() *url.URL {
	if len(e.List) == 0 {
		return nil
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()

	// If heap is empty, repopulate it
	if e.weightHeap.Len() == 0 {
		e.Initialize(WeightedRoundRobin)
	}

	for e.weightHeap.Len() > 0 {
		node := e.weightHeap.Top()
		if e.isServerHealthy(node.Index) {
			// Decrement weight (since we use negative weights for max-heap)
			node.Weight++
			// If weight is 0, pop it out of the heap
			if node.Weight == 0 {
				heap.Pop(e.weightHeap)
			} else {
				heap.Fix(e.weightHeap, 0)
			}
			e.DebugServers()
			return e.List[node.Index]
		} else {
			// Remove unhealthy server from heap
			heap.Pop(e.weightHeap)
		}
	}

	return nil
}

func MakeLoadBalancer(amount int, algorithm Algorithm) {
	lb := LoadBalancer{Algo: algorithm}
	ep := EndPoints{}


	// Server and router
	router := http.NewServeMux()
	server := http.Server{
    Addr:    "0.0.0.0:8090", // explicit: accept remote clients
    Handler: router,
}



	// Creating endpoints
	for i := 0; i < amount; i++ {
		ep.List = append(ep.List, createEndPoint(baseURL, i))
	}

	ep.activeConnections = make([]int, amount)

	ep.weights = make([]int, amount)
	for i := 0; i < amount; i++ {
		ep.weights[i] = (i%4) + 1
	}

	// Initialize health status - all servers start as healthy
	ep.healthStatus = make([]bool, amount)
	ep.lastCheck = make([]time.Time, amount)
	for i := range ep.healthStatus {
		ep.healthStatus[i] = true
		ep.lastCheck[i] = time.Now()
	}

	// Initialize connection tracking
	ep.Initialize(algorithm)

	router.HandleFunc("/loadbalancer/", makeRequest(&lb, &ep))

	// Listen and serve
	log.Fatal(server.ListenAndServe())
}

func makeRequest(lb *LoadBalancer, ep *EndPoints) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var selectedServer *url.URL
		serverIndex := -1
		// Get client IP for IP Hash algorithm
		clientIP := getClientIP(r)

		// Select server based on algorithm
		switch lb.Algo {
		case RoundRobin:
			selectedServer = ep.GetServerRR()

		case LeastConnections:
			selectedServer, serverIndex= ep.GetServerLC()
		case IPHash:
			selectedServer = ep.GetServerIPHash(clientIP)

		case WeightedRoundRobin:
			selectedServer = ep.GetServerWRR()

		default:
			selectedServer = ep.GetServerRR() // Default to round robin
		}

		if selectedServer == nil {
			http.Error(w, "No healthy servers available", http.StatusServiceUnavailable)
			return
		}

		// Create reverse proxy
		// NewSingleHost returns a pointer
		proxy := httputil.NewSingleHostReverseProxy(selectedServer)


		if lb.Algo == LeastConnections && serverIndex >= 0 {
			proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
				ep.decrementConnection(serverIndex)
			}
			proxy.ModifyResponse = func(resp *http.Response) error {
				resp.Body = &onCloseRC{
						ReadCloser: resp.Body,
						onClose: func() {
								ep.decrementConnection(serverIndex)
						},
				}
				return nil
			}
		}

		http.StripPrefix("/loadbalancer", proxy).ServeHTTP(w, r)
	}
}


func createEndPoint(endpoint string, index int) *url.URL {
	link := endpoint + strconv.Itoa(index)
	parsedURL, err := url.Parse(link)
	if err != nil {
		log.Printf("Error parsing URL %s: %v", link, err)
		return nil
	}
	return parsedURL
}

func getClientIP(r *http.Request) string {
    ip, _, _ := net.SplitHostPort(r.RemoteAddr)
    return ip
}