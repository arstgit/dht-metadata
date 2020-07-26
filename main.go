package main

import (
	"log"

	"github.com/derekchuank/dht-metadata/dht"
)

func main() {
	log.Println("Starting.")

	dht := dht.New()
	log.Printf("Dht status: %s", dht.YieldStatus())

	dht.Run()

	dht.FreeDht()
}
