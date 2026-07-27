// Command soak checks that the respondent rate limiter actually holds
// under load (M9-T3's AC), against a running instance.
//
// It is deliberately not a test: the limiters are per-process and
// in-memory, so a meaningful soak has to run against a deployed
// instance rather than an in-process one. Point it at staging.
//
// Point it at a bucketed endpoint. Not every respondent route has a
// limiter, and hammering one that does not is the easiest way to come
// away believing the limiter works when nothing was measured:
//
//	GET  /s/<id>/challenge   120/hour per IP
//	POST /s/<id>             30/hour per IP+survey with a solved challenge,
//	                         5/hour without one
//	GET  /s/<id>             no limiter — the page itself is free to fetch
//
// The challenge endpoint is the one to soak by default: it is bucketed,
// it writes nothing, and it exercises the same limiter type as the
// submit path.
//
//	go run ./tools/soak -url https://stg.tryearful.com/s/<survey-id>/challenge -n 200
//
// A healthy result is a wall of 429s after the bucket empties, and no
// 5xx at all: refusing cheaply is the whole point of the limiter, and a
// limiter that falls over under load has not refused anything.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

func main() {
	target := flag.String("url", "", "bucketed URL to hammer (see the package comment for which routes have limiters)")
	method := flag.String("method", http.MethodGet, "HTTP method to send")
	count := flag.Int("n", 200, "requests to send")
	concurrency := flag.Int("c", 10, "requests in flight")
	auth := flag.String("basic-auth", "", "user:pass for a staging instance behind Basic Auth")
	flag.Parse()

	if *target == "" {
		fmt.Fprintln(os.Stderr, "usage: soak -url https://host/s/<id>/challenge [-n 200] [-c 10] [-method GET]")
		os.Exit(2)
	}

	var (
		mu      sync.Mutex
		byCode  = map[int]int{}
		errored int
		slowest time.Duration
	)

	client := &http.Client{Timeout: 30 * time.Second}
	work := make(chan int)
	var wg sync.WaitGroup
	started := time.Now()

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				req, err := http.NewRequest(*method, *target, nil)
				if err != nil {
					mu.Lock()
					errored++
					mu.Unlock()
					continue
				}
				if *auth != "" {
					user, pass, _ := splitAuth(*auth)
					req.SetBasicAuth(user, pass)
				}
				begin := time.Now()
				resp, err := client.Do(req)
				elapsed := time.Since(begin)

				mu.Lock()
				if err != nil {
					errored++
				} else {
					byCode[resp.StatusCode]++
					resp.Body.Close()
				}
				if elapsed > slowest {
					slowest = elapsed
				}
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < *count; i++ {
		work <- i
	}
	close(work)
	wg.Wait()

	fmt.Printf("%d requests in %s (concurrency %d)\n", *count, time.Since(started).Round(time.Millisecond), *concurrency)
	for code, n := range byCode {
		fmt.Printf("  %d: %d\n", code, n)
	}
	if errored > 0 {
		fmt.Printf("  transport errors: %d\n", errored)
	}
	fmt.Printf("slowest response: %s\n", slowest.Round(time.Millisecond))

	switch {
	case byCode[429] == 0:
		// Not "inconclusive": nothing was demonstrated, and a run that
		// proves nothing while exiting 0 is how a limiter that stopped
		// applying goes unnoticed. Either -n is below the bucket or the
		// URL has no limiter behind it — both are the operator's to fix.
		fmt.Println("\nNo 429s: nothing was proven. Either -n is under the bucket size, " +
			"or this URL has no limiter (see the package comment for the ones that do).")
		os.Exit(1)
	case serverErrors(byCode) > 0:
		fmt.Printf("\n%d server errors: the limiter refused, but something behind it fell over.\n", serverErrors(byCode))
		os.Exit(1)
	default:
		fmt.Println("\nHealthy: the limiter refused cheaply and nothing behind it failed.")
	}
}

func serverErrors(byCode map[int]int) int {
	total := 0
	for code, n := range byCode {
		if code >= 500 {
			total += n
		}
	}
	return total
}

func splitAuth(raw string) (user, pass string, ok bool) {
	for i := 0; i < len(raw); i++ {
		if raw[i] == ':' {
			return raw[:i], raw[i+1:], true
		}
	}
	return raw, "", false
}
