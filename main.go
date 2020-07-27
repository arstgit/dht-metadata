package main

import (
	"log"

	"github.com/derekchuank/dht-metadata/dht"
)

func main() {
	log.Println("Starting.")

	dht := dht.New()

	dht.Run()

	dht.FreeDht()
}
