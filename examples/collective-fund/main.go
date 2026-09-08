// Standalone teaching example. No Nexum core or Comlink service imports.
package main

import (
	"errors"
	"fmt"
)

func require(ok bool, why string) {
	if !ok {
		panic(why)
	}
}
func refused(err error, why string) { require(err != nil, why); fmt.Println("REFUSED:", why) }

type Fund struct {
	target, total int
	contributions map[string]int
	consent       map[string]bool
	paid          bool
}

func (f *Fund) pledge(peer string, amount int) error {
	if f.paid || peer == "" || amount <= 0 || f.contributions[peer] != 0 || amount > f.target-f.total {
		return errors.New("pledge refused")
	}
	f.contributions[peer] = amount
	f.total += amount
	return nil
}
func (f *Fund) optIn(peer string) error {
	if f.contributions[peer] == 0 || f.paid {
		return errors.New("not a contributor")
	}
	f.consent[peer] = true
	return nil
}
func (f *Fund) settle(artifact []string) error {
	if f.paid || f.total != f.target {
		return errors.New("fund not ready")
	}
	for peer := range f.contributions {
		if !f.consent[peer] {
			return errors.New("missing opt-in")
		}
	}
	// Trusted milestone predicate: an agreed three-part artifact in order.
	expected := []string{"plan", "prototype", "review"}
	if len(artifact) != len(expected) {
		return errors.New("incomplete milestone")
	}
	for i := range expected {
		if artifact[i] != expected[i] {
			return errors.New("invalid milestone")
		}
	}
	f.paid = true
	return nil
}
func main() {
	f := Fund{target: 10, contributions: map[string]int{}, consent: map[string]bool{}}
	require(f.pledge("one", 4) == nil, "pledge failed")
	refused(f.pledge("one", 4), "duplicate contribution")
	refused(f.pledge("two", 7), "overfunding")
	refused(f.settle([]string{"plan", "prototype", "review"}), "underfunded settlement")
	require(f.pledge("two", 6) == nil, "second pledge failed")
	refused(f.optIn("stranger"), "nonparticipant opt-in")
	require(f.optIn("one") == nil, "opt-in failed")
	refused(f.settle([]string{"plan", "prototype", "review"}), "missing participant consent")
	require(f.optIn("two") == nil, "second opt-in failed")
	refused(f.settle([]string{"plan"}), "incomplete milestone")
	require(f.settle([]string{"plan", "prototype", "review"}) == nil, "valid settlement failed")
	refused(f.settle([]string{"plan", "prototype", "review"}), "duplicate payout")
	fmt.Println("PASS: target met, unanimous opt-in, milestone checked, demonstration fund settled")
}
