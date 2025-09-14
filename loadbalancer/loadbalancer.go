package loadbalancer

import (
	"container/heap"
	"crypto/md5"
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

var (
	baseURL = "http://localhost:808"
)

type Algorithm string

const (
	RoundRobin         Algorithm = "RoundRobin"
	LeastConnections   Algorithm = "LeastConnections"
	IPHash             Algorithm = "IPHash"
	WeightedRoundRobin Algorithm = "WeightedRoundRobin"
)

type LoadBalancer struct {
	RevProxy httputil.ReverseProxy
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
			connNode := ServerNode{
				Index:       i,
				Connections: 0,
				Weight:      0,
			}
			heap.Push(e.connHeap, connNode)
		}
		e.weightHeap = nil

	case WeightedRoundRobin:
		// Only initialize weight heap for weighted round robin
		e.weightHeap = ServerHeapConstructor(WeightComparer{})
		heap.Init(e.weightHeap)

		// Add all servers to weight heap (use negative weights for max-heap)
		for i := range e.List {
			weightNode := ServerNode{
				Index:       i,
				Connections: 0,
				Weight:      -e.weights[i], // Negative for max-heap
			}
			heap.Push(e.weightHeap, weightNode)
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
func (e *EndPoints) GetServerLC() *url.URL {
	if len(e.List) == 0 {
		return nil
	}

	e.mutex.Lock()
	// unlock mutex after function returns
	defer e.mutex.Unlock()

	if e.connHeap.Len() == 0 {
		e.Initialize(LeastConnections)
	}
	for e.connHeap.Len() > 0 {
		node := e.connHeap.Top()
		if e.isServerHealthy(node.Index) {
			e.activeConnections[node.Index]++
			node.Connections = e.activeConnections[node.Index]
			heap.Fix(e.connHeap,0)
			return e.List[node.Index]
		} else {
        // Remove unhealthy server from heap
        heap.Pop(e.connHeap)
    }
	}
	return nil
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
		Addr:    ":8090",
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

	router.HandleFunc("/loadbalancer", makeRequest(&lb, &ep))

	// Listen and serve
	log.Fatal(server.ListenAndServe())
}

func makeRequest(lb *LoadBalancer, ep *EndPoints) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var selectedServer *url.URL

		// Get client IP for IP Hash algorithm
		clientIP := getClientIP(r)

		// Select server based on algorithm
		switch lb.Algo {
		case RoundRobin:
			selectedServer = ep.GetServerRR()
		case LeastConnections:
			selectedServer = ep.GetServerLC()
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
		lb.RevProxy = *httputil.NewSingleHostReverseProxy(selectedServer)
		lb.RevProxy.ServeHTTP(w, r)
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