package loadbalancer

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
)

var (
	baseURL = "http://localhost:808"
)

type LoadBalancer struct {
	RevProxy httputil.ReverseProxy
}

type EndPoints struct {
	List []*url.URL
}

func (e *EndPoints) Shuffle() {
	temp := e.List[0]
	e.List = e.List[1:]
	e.List = append(e.List, temp)
}


func MakeLoadBalancer(amount int) {
	lb := LoadBalancer{}
	ep := EndPoints{}


	// Server and router
	router := http.NewServeMux()
	server := http.Server{
		Addr: ":8090",
		Handler: router,
	}


	// Creating endpoints
	for i := 0; i < amount; i++ {
		ep.List = append(ep.List, createEndPoint(baseURL, i))
	}

	router.HandleFunc("/loadbalancer", makeRequest(&lb, &ep))

	// Listen and serve
	log.Fatal(server.ListenAndServe())
}

func makeRequest(lb *LoadBalancer, ep *EndPoints) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		for !testServer(ep.List[0].String()) {
			ep.Shuffle()
		}
		lb.RevProxy = *httputil.NewSingleHostReverseProxy(ep.List[0])
		ep.Shuffle()
		lb.RevProxy.ServeHTTP(w,r)
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


func testServer(endpoint string) bool {
	response, err := http.Get(endpoint)
	if err != nil {
		return false
	}
	defer response.Body.Close()

	return response.StatusCode == http.StatusOK
}