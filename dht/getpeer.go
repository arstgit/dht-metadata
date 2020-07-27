package dht

import (
	"encoding/hex"
	"log"
)

const examplehash = "0271e29577a33ce47dc38c78240f2363bcb4ef7c"

type infohash [20]byte

func strToInfohash(s string) infohash {
	if len(s) != 40 {
		log.Fatal("infohash string len not 40")
	}

	var res [20]byte

	n, err := hex.Decode(res[:], []byte(s))
	if err != nil {
		log.Fatal(err)
	}
	if n != 20 {
		log.Fatal("decoded return n not 20")
	}

	return res
}
