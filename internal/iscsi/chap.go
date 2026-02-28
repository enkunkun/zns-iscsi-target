package iscsi

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// CHAPChallenge holds a CHAP challenge identifier and challenge bytes.
type CHAPChallenge struct {
	ID        byte   // CHAP identifier
	Challenge []byte // Random challenge bytes
}

// NewCHAPChallenge generates a new random CHAP challenge.
func NewCHAPChallenge() (*CHAPChallenge, error) {
	id := make([]byte, 1)
	if _, err := rand.Read(id); err != nil {
		return nil, fmt.Errorf("generating CHAP ID: %w", err)
	}
	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		return nil, fmt.Errorf("generating CHAP challenge: %w", err)
	}
	return &CHAPChallenge{
		ID:        id[0],
		Challenge: challenge,
	}, nil
}

// ChallengeHex returns the challenge bytes as a hex string with "0x" prefix.
func (c *CHAPChallenge) ChallengeHex() string {
	return "0x" + hex.EncodeToString(c.Challenge)
}

// VerifyResponse verifies a CHAP response against the secret.
// The expected response is: MD5(ID || Secret || Challenge).
// The response string should be "0x" + hex digits.
func (c *CHAPChallenge) VerifyResponse(secret, response string) bool {
	expected := computeCHAPResponse(c.ID, secret, c.Challenge)
	// Parse response
	resp := response
	if len(resp) > 2 && (resp[:2] == "0x" || resp[:2] == "0X") {
		resp = resp[2:]
	}
	respBytes, err := hex.DecodeString(resp)
	if err != nil {
		return false
	}
	return hex.EncodeToString(respBytes) == expected
}

// ComputeCHAPResponse computes the CHAP response for the given ID, secret,
// and challenge. This is the MD5(ID || Secret || Challenge) as hex.
func ComputeCHAPResponse(id byte, secret string, challenge []byte) string {
	return "0x" + computeCHAPResponse(id, secret, challenge)
}

// computeCHAPResponse computes the MD5 hash of (id || secret || challenge).
func computeCHAPResponse(id byte, secret string, challenge []byte) string {
	h := md5.New()
	h.Write([]byte{id})
	h.Write([]byte(secret))
	h.Write(challenge)
	return hex.EncodeToString(h.Sum(nil))
}

// ParseCHAPParams extracts CHAP key-value pairs from negotiation parameters.
type CHAPParams struct {
	Algorithm string // CHAP_A: "5" for MD5
	Identifier string // CHAP_I: numeric identifier
	Name      string // CHAP_N: initiator name
	Response  string // CHAP_R: hex response
}

// ParseCHAPParams extracts CHAP parameters from a key-value map.
func ParseCHAPParams(kv map[string]string) CHAPParams {
	return CHAPParams{
		Algorithm:  kv["CHAP_A"],
		Identifier: kv["CHAP_I"],
		Name:       kv["CHAP_N"],
		Response:   kv["CHAP_R"],
	}
}
