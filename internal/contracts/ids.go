package contracts

import (
	"crypto/rand"
	"encoding/hex"
)

type ID string

func NewID() ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return ID(hex.EncodeToString(b[:]))
}
