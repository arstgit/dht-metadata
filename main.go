package main

import (
	"log"

	"github.com/derekchuank/dht-metadata/dht"
)

func main() {
	dht := dht.New()

	dht.Run()

	log.Fatal("unexpected position")
}
