// Standalone teaching example. No Nexum core or Comlink service imports.
package main

import (
	"errors"
	"fmt"
	"sort"
)

func require(ok bool, why string) {
	if !ok {
		panic(why)
	}
}
func refused(err error, why string) { require(err != nil, why); fmt.Println("REFUSED:", why) }

type Bounty struct {
	input                    []int
	optedIn, locked, settled bool
}

func (b *Bounty) agree() { b.optedIn = true }
func (b *Bounty) lock()  { b.locked = true }
func (b *Bounty) submit(result []int) error {
	if !b.optedIn || !b.locked || b.settled {
		return errors.New("agreement not ready")
	}
	expected := append([]int(nil), b.input...)
	sort.Ints(expected)
	if len(result) != len(expected) {
		return errors.New("missing or extra values")
	}
	for i := range expected {
		if expected[i] != result[i] {
			return errors.New("incorrect result")
		}
	}
	b.settled = true
	return nil
}
func main() {
	b := Bounty{input: []int{9, 2, 5, 2}}
	refused(b.submit([]int{2, 2, 5, 9}), "work before consent and lock")
	b.agree()
	b.lock()
	refused(b.submit([]int{2, 5, 9}), "missing duplicate")
	refused(b.submit([]int{2, 2, 9, 5}), "unsorted result")
	require(!b.settled, "bad work settled")
	require(b.submit([]int{2, 2, 5, 9}) == nil, "valid work refused")
	refused(b.submit([]int{2, 2, 5, 9}), "second settlement")
	fmt.Println("PASS: terms agreed, result checked, demonstration bounty settled")
}
