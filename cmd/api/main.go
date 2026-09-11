// scribe api -- v0.2
//
// Same contract as before: take a URL, remember it, hand it back. What
// changed is where "remember it" happens. Jobs used to live in a map that
// died with the process. Now they live in Postgres, which is why two
// replicas can finally agree with each other.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver with database/sql
)

// Job is one piece of work: a URL someone wants transcribed.
type Job struct {
	ID        int       `json:"id"`
	URL       string    `json:"url"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}

// store talks to Postgres. No mutex, no map -- Postgres does the
// taking-turns itself, which is most of why databases exist.
type store struct {
	db *sql.DB
}

func newStore(db *sql.DB) *store {
	return &store{db: db}
}

const createJobsTable = `
CREATE TABLE IF NOT EXISTS jobs (
	id         BIGSERIAL PRIMARY KEY,
	url        TEXT NOT NULL,
	state      TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

func (s *store) add(ctx context.Context, url string) (Job, error) {
	var j Job
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO jobs (url, state) VALUES ($1, 'queued')
		 RETURNING id, url, state, created_at`,
		url,
	).Scan(&j.ID, &j.URL, &j.State, &j.CreatedAt)
	return j, err
}

func (s *store) get(ctx context.Context, id int) (Job, bool, error) {
	var j Job
	err := s.db.QueryRowContext(ctx,
		`SELECT id, url, state, created_at FROM jobs WHERE id = $1`, id,
	).Scan(&j.ID, &j.URL, &j.State, &j.CreatedAt)
	switch {
	case err == sql.ErrNoRows:
		return Job{}, false, nil
	case err != nil:
		return Job{}, false, err
	}
	return j, true, nil
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("cannot open database: %v", err)
	}
	defer db.Close()

	// Fail fast at startup if Postgres is unreachable, rather than let every
	// request find out one at a time.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("cannot reach postgres: %v", err)
	}
	if _, err := db.ExecContext(ctx, createJobsTable); err != nil {
		log.Fatalf("cannot create jobs table: %v", err)
	}

	st := newStore(db)
	mux := http.NewServeMux()

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

		j, err := st.add(r.Context(), body.URL)
		if err != nil {
			log.Printf("insert job: %v", err)
			http.Error(w, "could not save job", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusAccepted, j)
	})

	mux.HandleFunc("GET /jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id must be a number", http.StatusBadRequest)
			return
		}

		j, ok, err := st.get(r.Context(), id)
		if err != nil {
			log.Printf("get job: %v", err)
			http.Error(w, "could not read job", http.StatusInternalServerError)
			return
		}
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
		ReadHeaderTimeout: 5 * time.Second,
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
