// Standalone teaching example. No Nexum core or Comlink service imports.
package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

func require(ok bool, why string) {
	if !ok {
		panic(why)
	}
}
func refused(err error, why string) { require(err != nil, why); fmt.Println("REFUSED:", why) }

type Offer struct {
	key                             []byte
	consent, acknowledged, released bool
}

func (o *Offer) release() ([]byte, error) {
	if !o.consent || !o.acknowledged || o.released {
		return nil, errors.New("release conditions unmet")
	}
	o.released = true
	return append([]byte(nil), o.key...), nil
}
func aead(key []byte) cipher.AEAD {
	block, err := aes.NewCipher(key)
	require(err == nil, "cipher setup")
	a, err := cipher.NewGCM(block)
	require(err == nil, "GCM setup")
	return a
}
func main() {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require(err == nil, "randomness unavailable")
	sender := aead(key)
	nonce := make([]byte, sender.NonceSize())
	_, err = rand.Read(nonce)
	require(err == nil, "nonce unavailable")
	terms := []byte("report/v1; recipient=peer; purpose=research")
	report := []byte("Synthetic research report for the local demo.")
	ciphertext := sender.Seal(nil, nonce, report, terms)
	offer := Offer{key: key}
	_, err = offer.release()
	refused(err, "key release before consent")
	offer.consent = true
	_, err = offer.release()
	refused(err, "key release before terms acknowledged")
	offer.acknowledged = true
	recipientKey, err := offer.release()
	require(err == nil, "release failed")
	recipient := aead(recipientKey)
	tampered := append([]byte(nil), ciphertext...)
	tampered[0] ^= 1
	_, err = recipient.Open(nil, nonce, tampered, terms)
	refused(err, "tampered report")
	_, err = recipient.Open(nil, nonce, ciphertext, []byte("different terms"))
	refused(err, "substituted terms")
	plaintext, err := recipient.Open(nil, nonce, ciphertext, terms)
	require(err == nil && bytes.Equal(plaintext, report), "decryption failed")
	_, err = offer.release()
	refused(err, "duplicate key release")
	fmt.Println("PASS: consent and terms gate key release; authenticated report recovered")
}
