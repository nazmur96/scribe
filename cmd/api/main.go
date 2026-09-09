// scribe api -- v0.1
//
// Takes a URL, remembers it, hands it back. That is all it does today.
//
// There is no database yet: jobs live in a map that dies with the process.
// That is on purpose. Today's goal is to prove the path from "code on my
// laptop" to "container running on the cluster". Adding Postgres at the same
// time would mean two new things breaking at once, with no way to tell which.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// Job is one piece of work: a URL someone wants transcribed.
//
// The `json:"..."` bits tell Go what to call each field when it turns this
// into JSON. Without them you would get "ID" and "URL" instead of "id" and
// "url".
type Job struct {
	ID        int       `json:"id"`
	URL       string    `json:"url"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}

// store holds the jobs. A map, plus a lock.
//
// The lock matters: an HTTP server handles requests at the same time, on
// different threads. Two of them writing to the same map at once will crash
// Go outright -- it detects this and refuses to continue. The lock makes them
// take turns.
//
// This whole type disappears on day 3, when Postgres takes over. Postgres
// handles the taking-turns part itself, which is most of why databases exist.
type store struct {
	mu     sync.Mutex
	jobs   map[int]Job
	nextID int
}

func newStore() *store {
	return &store{jobs: make(map[int]Job), nextID: 1}
}

func (s *store) add(url string) Job {
	s.mu.Lock()
	defer s.mu.Unlock() // runs when this function returns, even if it panics

	j := Job{
		ID:        s.nextID,
		URL:       url,
		State:     "queued",
		CreatedAt: time.Now().UTC(),
	}
	s.jobs[j.ID] = j
	s.nextID++
	return j
}

func (s *store) get(id int) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	return j, ok
}

func main() {
	// Read the port from the environment, fall back to 8080.
	//
	// Hardcoding a port is fine until something else already uses it. Reading
	// it from the environment is how every container platform expects to
	// configure a service, Kubernetes included.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	st := newStore()
	mux := http.NewServeMux()

	// Since Go 1.22, ServeMux understands "METHOD /path/{name}". Before that
	// everyone reached for a third-party router. We do not need one.
	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "body must be JSON", http.StatusBadRequest)
			return
		}
		if body.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}

		j := st.add(body.URL)

		// 202 Accepted, not 200 OK. It means "I have taken this, but I have
		// not done it yet" -- which is exactly true, and will still be true
		// when a real worker takes twenty minutes over it.
		writeJSON(w, http.StatusAccepted, j)
	})

	mux.HandleFunc("GET /jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id must be a number", http.StatusBadRequest)
			return
		}

		j, ok := st.get(id)
		if !ok {
			http.Error(w, "no such job", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, j)
	})

	// Kubernetes will call this to decide whether the pod is alive. It must
	// stay cheap and must never touch a database -- a health check that talks
	// to Postgres will report the app as dead whenever Postgres hiccups, and
	// Kubernetes will helpfully restart a perfectly healthy app.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second, // refuse clients that connect and then say nothing
	}

	log.Printf("scribe api listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
