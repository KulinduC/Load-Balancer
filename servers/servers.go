package servers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type ServerList struct {
	Ports []int
	mutex sync.Mutex
}


func (s *ServerList) Populate(amount int) {

	if amount >= 10 {
		log.Fatal("Amount of Ports cant exceed 10")
	}

	for x := 0; x < amount; x++ {
		s.Ports = append(s.Ports, x)
	}

}

func (s *ServerList) Pop() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	port := s.Ports[0]
	s.Ports = s.Ports[1:]
	return port

}

func RunServers(amount int) {
	sList := ServerList{}
	sList.Populate(amount)

	// Waitgroup
	wg := sync.WaitGroup{}
	wg.Add(amount)
	defer wg.Wait()

	for x := 0; x < amount; x++ {
		go makeServers(&sList, &wg)
	}
}

func makeServers(s *ServerList, wg *sync.WaitGroup) {
	defer wg.Done()
	port := s.Pop()



	// Each server gets its own router
	router := http.NewServeMux()

	server := &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:808%d", port),
		Handler:      router,
		ReadTimeout:  300 * time.Second, // 5 minutes for large file uploads
		WriteTimeout: 300 * time.Second, // 5 minutes for large file downloads
		IdleTimeout:  120 * time.Second, // 2 minutes idle timeout
	}


	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Server %d", port)
	})

	router.HandleFunc("/largefile", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Starting simulated long download from server %d...", port)

    timer := time.NewTimer(15 * time.Second)
    defer timer.Stop()

    // Wait for either the timer to expire or the request context to be canceled
    select {
    case <-timer.C:
        // The download finished successfully
        log.Printf("...long download from server %d finished.", port)
        w.WriteHeader(http.StatusOK)
    case <-r.Context().Done():
        // The request context was canceled
        log.Printf("...download from server %d canceled.", port)
    }
	})

	router.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte("400 - Server Shut Down!"))
		server.Shutdown(context.Background())
	})


	server.ListenAndServe()
}
