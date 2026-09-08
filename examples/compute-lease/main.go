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

type Slot struct {
	owner      string
	start, end int
	used       bool
}
type Scheduler struct {
	mu    sync.Mutex
	slots []*Slot
}

func (s *Scheduler) reserve(owner string, start, end int) (*Slot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner == "" || start < 0 || start >= end {
		return nil, errors.New("invalid reservation")
	}
	for _, slot := range s.slots {
		if start < slot.end && slot.start < end {
			return nil, errors.New("overlap")
		}
	}
	slot := &Slot{owner: owner, start: start, end: end}
	s.slots = append(s.slots, slot)
	return slot, nil
}
func (s *Scheduler) run(slot *Slot, owner string, now int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	admitted := false
	for _, entry := range s.slots {
		if entry == slot {
			admitted = true
		}
	}
	if !admitted || owner != slot.owner || now < slot.start || now >= slot.end || slot.used {
		return 0, errors.New("dispatch refused")
	}
	slot.used = true
	// Fixed bounded CPU fixture. Never executes caller-supplied code.
	total := 0
	for i := 1; i <= 100; i++ {
		total += i
	}
	return total, nil
}
func main() {
	s := Scheduler{}
	slot, err := s.reserve("researcher", 10, 20)
	require(err == nil, "reservation failed")
	_, err = s.reserve("peer", 15, 25)
	refused(err, "overlapping reservation")
	_, err = s.reserve("peer", 20, 30)
	require(err == nil, "adjacent slot refused")
	_, err = s.run(slot, "stranger", 12)
	refused(err, "wrong owner")
	_, err = s.run(slot, "researcher", 9)
	refused(err, "early dispatch")
	_, err = s.run(slot, "researcher", 20)
	refused(err, "expired dispatch")
	value, err := s.run(slot, "researcher", 12)
	require(err == nil && value == 5050, "work failed")
	_, err = s.run(slot, "researcher", 13)
	refused(err, "replayed dispatch")
	fmt.Println("PASS: exclusive slot reserved and bounded CPU task completed")
}
