// Standalone teaching example. No Nexum core or Comlink service imports.
package main

import (
	"errors"
	"fmt"
	"sync"
)

func require(ok bool, why string) {
	if !ok {
		panic(why)
	}
}
func refused(err error, why string) { require(err != nil, why); fmt.Println("REFUSED:", why) }

type Grant struct {
	mu                     sync.Mutex
	peer                   string
	budget, spent, expires int
	revoked                bool
}

func (g *Grant) use(peer, tool string, now int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Fixed trusted price; a caller cannot choose its own charge.
	const cost = 3
	if g.revoked || now >= g.expires || peer != g.peer || tool != "lookup" || g.budget-g.spent < cost {
		return errors.New("outside grant")
	}
	g.spent += cost
	return nil
}
func (g *Grant) revoke() { g.mu.Lock(); defer g.mu.Unlock(); g.revoked = true }
func main() {
	g := Grant{peer: "specialist", budget: 6, expires: 20}
	refused(g.use("stranger", "lookup", 10), "wrong delegate")
	refused(g.use("specialist", "purchase", 10), "unapproved tool")
	refused(g.use("specialist", "lookup", 20), "expired grant")
	require(g.spent == 0, "refused action charged")
	var wg sync.WaitGroup
	successes := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); successes <- (g.use("specialist", "lookup", 10) == nil) }()
	}
	wg.Wait()
	close(successes)
	count := 0
	for ok := range successes {
		if ok {
			count++
		}
	}
	require(count == 2 && g.spent == 6, "concurrent spending exceeded budget")
	fresh := Grant{peer: "specialist", budget: 6, expires: 20}
	fresh.revoke()
	refused(fresh.use("specialist", "lookup", 10), "revoked grant")
	fmt.Println("PASS: delegation respects tool, expiry, revocation and concurrent budget limits")
}
